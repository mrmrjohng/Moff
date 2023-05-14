package discord

import (
	"context"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"go.uber.org/atomic"
	"gonum.org/v1/gonum/stat/combin"
	"gopkg.in/fatih/set.v0"
	"gorm.io/gorm"
	"math/rand"
	"moff.io/moff-social/internal/aws"
	"moff.io/moff-social/internal/cache"
	"moff.io/moff-social/internal/config"
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/pkg/common"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"strconv"
	"strings"
	"sync"
	"time"
)

type QuizGameManager struct {
	ctx          context.Context
	rwLock       sync.RWMutex
	lotteries    map[string]*quizGameLottery
	ongoingGames map[string]*quizGame
}

var (
	initQuizGameManagerOnce sync.Once
	internalQuizGameManager *QuizGameManager
)

func NewQuizGameManager() *QuizGameManager {
	initQuizGameManagerOnce.Do(func() {
		internalQuizGameManager = &QuizGameManager{
			lotteries:    make(map[string]*quizGameLottery),
			ongoingGames: make(map[string]*quizGame),
		}

	})
	return internalQuizGameManager
}

func (m *QuizGameManager) Start(ctx context.Context) {
	m.ctx = ctx
	m.loadLotteries(ctx)
}

func (m *QuizGameManager) loadLotteries(ctx context.Context) {
	// 启动加载db的未开始lottery
	lotteries, err := database.DiscordQuizGameLottery{}.SelectUnfinished()
	if err != nil {
		panic(err)
	}
	for _, l := range lotteries {
		// 获取对应的游戏
		games, err := database.DiscordQuizGame{}.SelectByLotteryIds([]string{l.LotteryID})
		if err != nil {
			panic(err)
		}
		m.AddLottery(l)
		for _, g := range games {
			if err := m.AddGame(g); err != nil {
				panic(err)
			}
		}
	}
	log.Info("Loaded database lotteries and games...")
}

func (m *QuizGameManager) GetGame(gameID string) *quizGame {
	m.rwLock.RLock()
	defer m.rwLock.RUnlock()
	return m.ongoingGames[gameID]
}

func (m *QuizGameManager) AddLottery(lottery *database.DiscordQuizGameLottery) {
	m.rwLock.Lock()
	defer m.rwLock.Unlock()
	gl := m.lotteries[lottery.LotteryID]
	if gl != nil {
		gl.DiscordQuizGameLottery = lottery
		return
	}
	gameLottery := newQuizGameLottery(m.ctx, lottery)
	m.lotteries[lottery.LotteryID] = gameLottery
	go func() {
		gameLottery.Start(m.ctx)
		m.onLotteryFinished(gameLottery)
	}()
}

func (m *QuizGameManager) onLotteryFinished(lottery *quizGameLottery) {
	m.rwLock.Lock()
	defer m.rwLock.Unlock()
	delete(m.lotteries, lottery.LotteryID)
	for _, game := range lottery.games {
		delete(m.ongoingGames, game.GameID)
	}
}

func (m *QuizGameManager) AddGame(game *database.DiscordQuizGame) error {
	m.rwLock.Lock()
	defer m.rwLock.Unlock()
	gl := m.lotteries[game.LotteryID]
	if gl == nil {
		return errors.ErrorfAndReport("game lottery %v not found when add game", game.LotteryID)
	}

	qa := newQuizGame(gl.ctx, game)
	if err := gl.AddGame(qa); err != nil {
		return err
	}
	m.ongoingGames[qa.GameID] = qa
	return nil
}

func (m *QuizGameManager) RemoveGame(game *database.DiscordQuizGame) error {
	m.rwLock.Lock()
	defer m.rwLock.Unlock()

	gl := m.lotteries[game.LotteryID]
	if gl == nil {
		return errors.ErrorfAndReport("game lottery %v not found when remove game", game.LotteryID)
	}
	if err := gl.RemoveGame(game); err != nil {
		return err
	}
	delete(m.ongoingGames, game.GameID)
	return nil
}

type quizGameLottery struct {
	ctx        context.Context
	cancelFunc context.CancelFunc

	rwLock sync.RWMutex

	// 等待完成的游戏数
	wait2BeFinishedGameCount atomic.Int64
	// 当游戏全部完成时，开奖结算的通知队列
	finalizeLotteryChan chan struct{}
	// 参与开奖的小游戏，用于最终计算获胜者
	games   []*quizGame
	gameSet map[string]bool

	*database.DiscordQuizGameLottery
	quest *database.CommunityQuestTemplate
}

func newQuizGameLottery(ctx context.Context, lottery *database.DiscordQuizGameLottery) *quizGameLottery {
	ctx, cancelFunc := context.WithCancel(ctx)
	l := &quizGameLottery{
		ctx:                    ctx,
		cancelFunc:             cancelFunc,
		finalizeLotteryChan:    make(chan struct{}, 2),
		gameSet:                map[string]bool{},
		DiscordQuizGameLottery: lottery,
	}
	return l
}

func (l *quizGameLottery) AddGame(game *quizGame) error {
	l.rwLock.Lock()
	defer l.rwLock.Unlock()
	found := l.gameSet[game.GameID]
	if found {
		return l.updateGameLocked(game)
	}
	return l.addNewGameLocked(game)
}

func (l *quizGameLottery) RemoveGame(game *database.DiscordQuizGame) error {
	l.rwLock.Lock()
	defer l.rwLock.Unlock()

	found := l.gameSet[game.GameID]
	if !found {
		return nil
	}
	var games []*quizGame
	for _, g := range l.games {
		if g.GameID == game.GameID {
			if err := g.Terminate(); err != nil {
				return err
			}
			continue
		}
		games = append(games, g)
	}
	l.games = games
	return nil
}

func (l *quizGameLottery) addGameLocked(game *quizGame) error {
	found := l.gameSet[game.GameID]
	if found {
		return l.updateGameLocked(game)
	}
	return l.addNewGameLocked(game)
}

func (l *quizGameLottery) addNewGameLocked(game *quizGame) error {
	l.gameSet[game.GameID] = true
	l.games = append(l.games, game)
	return l.StartGame(game)
}

func (l *quizGameLottery) updateGameLocked(game *quizGame) error {
	for i := 0; i < len(l.games); i++ {
		currGame := l.games[i]
		if currGame.GameID == game.GameID {
			if err := currGame.Terminate(); err != nil {
				return err
			}
			l.games[i] = game
			return l.StartGame(game)
		}
	}
	return errors.Errorf("game %v not found in lottery", game.GameID)
}

func (l *quizGameLottery) StartGame(game *quizGame) error {
	l.wait2BeFinishedGameCount.Inc()

	go func() {
		game.Play(l.ctx)
		l.wait2BeFinishedGameCount.Dec()
		if l.wait2BeFinishedGameCount.Load() == 0 {
			l.finalizeLotteryChan <- struct{}{}
		}
	}()
	return nil
}

func (l *quizGameLottery) Terminate() bool {
	// 如已添加游戏，不允许终止
	if len(l.games) != 0 {
		return false
	}
	if l.cancelFunc != nil {
		l.cancelFunc()
	}
	return true
}

func (l *quizGameLottery) Start(ctx context.Context) {
	log.Infof("Lottery %v running...", l.LotteryID)
	l.ctx, l.cancelFunc = context.WithCancel(ctx)
	ticker := time.NewTicker(time.Hour)
	for {
		select {
		case <-l.ctx.Done():
			log.Warnf("lottery %v canceled", l.LotteryID)
			return
		case <-ticker.C:
			if len(l.games) == 0 {
				continue
			}
			if l.wait2BeFinishedGameCount.Load() == 0 {
				log.Warnf("lottery %v terminating", l.LotteryID)
				return
			}
		case <-l.finalizeLotteryChan:
			log.Infof("Lottery %v started...", l.LotteryID)
			<-time.After(time.Second * 5)
			totalWinners := l.calculateAllQuizGamesWinners()
			finalWinners := l.finalizeQuizGamesWinners(totalWinners)
			l.announceFinished(finalWinners)
			return
		}
	}
}

func (l *quizGameLottery) finalizeQuizGamesWinners(winners []interface{}) []interface{} {
	// 不足预期数量
	totalWinnerNum := len(winners)
	if totalWinnerNum <= l.AllowedWinnerNum {
		return winners
	}
	// 超过预期数量，伪随机获取
	var (
		dedup   = make(map[int]bool)
		results []interface{}
	)
	rand.Seed(time.Now().UnixNano())
	for {
		if len(results) >= l.AllowedWinnerNum {
			break
		}
		idx := rand.Intn(totalWinnerNum)
		ok := dedup[idx]
		if ok {
			continue
		}
		dedup[idx] = true
		results = append(results, winners[idx])
	}
	return results
}

func (l *quizGameLottery) calculateAllQuizGamesWinners() []interface{} {
	gameNum := len(l.games)
	if gameNum < 2 {
		return *l.games[0].Winners
	}
	// 胜者条件检查，不一致则自动降级
	if len(l.games) < l.TotalQuizNum {
		l.TotalQuizNum = len(l.games)
	}
	if l.WinnerRequiredCorrectQuizNum > l.TotalQuizNum {
		l.WinnerRequiredCorrectQuizNum = l.TotalQuizNum
	}
	// 计算答题胜者
	winners := make(map[string]struct{})
	gen := combin.NewCombinationGenerator(l.TotalQuizNum, l.WinnerRequiredCorrectQuizNum)
	for gen.Next() {
		combination := gen.Combination(nil)
		l.calculateQuizGamesWinnersInCombination(combination, winners)
	}
	res := make([]interface{}, 0)
	for winner := range winners {
		res = append(res, winner)
	}
	return res
}

func (l *quizGameLottery) calculateQuizGamesWinnersInCombination(combination []int, winners map[string]struct{}) {
	var (
		intersection = set.New(set.NonThreadSafe)
	)
	// 计算胜者
	addIntoSet(&intersection, *l.games[0].Winners)
	for _, comb := range combination {
		currSet := set.New(set.NonThreadSafe)
		addIntoSet(&currSet, *l.games[comb].Winners)
		intersection = set.Intersection(intersection, currSet)
	}
	// 保存胜者
	list := intersection.List()
	for _, e := range list {
		winner := e.(string)
		winners[winner] = struct{}{}
	}
}

func (l *quizGameLottery) announceFinished(winners []interface{}) {
	now := time.Now()
	l.Winners = winners
	l.EndedAt = &now
	log.Infof("Lottery %v announcing %v winners...", l.LotteryID, len(winners))
	var (
		maxTry   = 3
		finished bool
		notified bool
	)
	for i := 0; i < maxTry; i++ {
		if !finished {
			finished = l.finishLottery()
		}
		if !notified {
			notified = l.notifyLotteryFinished()
		}
		if finished && notified {
			return
		}
	}
}

func (l *quizGameLottery) finishLottery() bool {
	// 终结开奖
	if err := l.UpdateFinished(); err != nil {
		log.Error(err)
		return false
	}
	// 添加任务奖励
	if err := l.createCommunityQuest(); err != nil {
		log.Error(err)
		return false
	}
	// 发放奖励通知
	//err := l.triggerQARewardGeneration()
	return true
}

func (l *quizGameLottery) triggerQARewardGeneration() error {
	var (
		err       error
		batch     []string
		batchSize = 100
	)
	for _, winner := range l.Winners {
		batch = append(batch, winner.(string))
		if len(batch) == batchSize {
			notification := newDiscordUserCommunityQuestRewardFromWhitelist(l.quest, batch).Marshal()
			batch = []string{}
			e := aws.Client.MultiTrySendMessageToSQS(l.ctx, config.Global.DiscordBot.MessageQueues.GenerateCommunityQuestRewardsQueue,
				notification, 3)
			if e != nil {
				err = e
				log.Error(e)
				continue
			}
		}
	}
	return err
}

func (l *quizGameLottery) createCommunityQuest() error {
	// 添加奖励白名单
	endMillis := l.EndedAt.Add(time.Hour*24*3).UnixNano() / 1e6
	startMillis := l.EndedAt.UnixNano() / 1e6
	date := time.UnixMilli(startMillis).UTC().Format("Jan.02")

	whitelist := database.CommunityQuestWhitelist{
		WhitelistID:   uuid.NewString(),
		WhitelistName: fmt.Sprintf("%v Q&A Winners", date),
	}
	var winners []*database.CommunityQuestWhitelistUser
	for _, did := range l.Winners {
		winners = append(winners, &database.CommunityQuestWhitelistUser{
			WhitelistID:  whitelist.WhitelistID,
			IdentityType: database.CommunityQuestWhitelistUserIdentityTypeDiscordIds,
			Identity:     did.(string),
		})
	}
	// 添加任务
	quest := &database.CommunityQuestTemplate{
		QuestID:                common.SHA256HexString([]byte(l.LotteryID)),
		QuestName:              fmt.Sprintf("%v Q&A", date),
		Dragonball:             l.RewardAmount,
		StartTime:              &startMillis,
		EndTime:                &endMillis,
		ClaimableDurationHours: 24 * 15,
		RequirementsType:       database.CommunityQuestTemplateRequirementsTypeWhitelist,
		Requirements: map[string]interface{}{
			"whitelist_id": whitelist.WhitelistID,
		},
	}
	if l.TotalQuizNum == l.WinnerRequiredCorrectQuizNum {
		quest.QuestDescription = "Answer all Q&A correctly"
	} else {
		quest.QuestDescription = fmt.Sprintf("Answer %v out of %v Q&A correctly",
			l.WinnerRequiredCorrectQuizNum, l.TotalQuizNum)
	}
	err := database.PublicPostgres.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(quest).Error; err != nil {
			return err
		}
		if err := tx.Create(whitelist).Error; err != nil {
			return err
		}
		return tx.Create(&winners).Error
	})
	return errors.WrapAndReport(err, "create community quest to pg")
}

func (l *quizGameLottery) notifyLotteryFinished() bool {
	// 缓存胜者
	if len(l.Winners) > 0 {
		var winnerFields []interface{}
		for _, winner := range l.Winners {
			winnerFields = append(winnerFields, winner, 1)
		}
		err := cache.Redis.HMSet(l.ctx, fmt.Sprintf("%v%v", quizGameLotteryWinnersCacheKeyPrefix, l.LotteryID), winnerFields...).Err()
		if err != nil {
			log.Error(errors.WithMessageAndReport(err, "cache lottery winners"))
			return false
		}
	}

	log.Infof("Lottery %v notifying %v winners...", l.LotteryID, len(l.Winners))
	content := "@everyone\n There are " + strconv.Itoa(len(l.Winners)) +
		" winners of tonight's quick quiz!  \U0001F973 \n\nPlease make sure that you've connected your wallet at https://moff.io/, and connected your discord account to your account. Or you will NOT receive the reward.\n\nThe rewards will be distributed in 3 days in the rewards page."
	_, err := session.ChannelMessageSendComplex(l.games[0].ChannelID, &discordgo.MessageSend{
		Content: content,
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "Click this button to check your result.",
						Style:    discordgo.DangerButton,
						CustomID: fmt.Sprintf("%v%v", discordQuizGameLotteryCheckResultCustomIDPrefix, l.LotteryID),
					},
				},
			},
		},
	})
	if err != nil {
		log.Error(errors.WrapAndReport(err, "announce game lottery winners"))
		return false
	}
	return true
}

func addIntoSet(s *set.Interface, ids []interface{}) {
	for _, id := range ids {
		(*s).Add(id)
	}
}

type quizGame struct {
	ctx        context.Context
	cancelFunc context.CancelFunc

	// 正确答案的自定义id
	CorrectAnswerOptionCustomID string

	*database.DiscordQuizGame

	daemonID string
}

func newQuizGame(ctx context.Context, game *database.DiscordQuizGame) *quizGame {
	cancelCtx, cancelFunc := context.WithCancel(ctx)
	var optionCustomID string
	for i, opt := range game.AnswerOptions {
		if game.CorrectAnswerOption == opt {
			optionCustomID = strconv.Itoa(i + 1)
			break
		}
	}
	return &quizGame{
		ctx:                         cancelCtx,
		cancelFunc:                  cancelFunc,
		CorrectAnswerOptionCustomID: optionCustomID,
		DiscordQuizGame:             game,
		daemonID:                    common.NewCutUUIDString(),
	}
}

func (g *quizGame) Expired() bool {
	timePassed := int(time.Since(g.SendQuizAt).Seconds())
	return timePassed > g.TimeLimitSec
}

func (g *quizGame) Play(ctx context.Context) {
	if g.Status.Is(database.DiscordQuizGameStatusFinished) {
		log.Debugf("game %v already finished", g.GameID)
		return
	}
	g.ctx, g.cancelFunc = context.WithCancel(ctx)
	defer func() {
		if i := recover(); i != nil {
			log.Error(errors.ErrorfAndReport("quiz game panic:%v", i))
		}
		g.cancelFunc()
		log.Infof("[%v] - quiz game %v daemon terminated...", g.daemonID, g.GameID)
	}()
	log.Infof("[%v] - quiz game %v daemon running...", g.daemonID, g.GameID)
	// TODO 游戏已经结束
	if int(time.Since(g.SendQuizAt).Seconds()) >= g.TimeLimitSec {

	}
	g.upcoming()
	g.ongoing()
}

func (g *quizGame) upcoming() {
	if !g.Status.Is(database.DiscordQuizGameStatusNotStarted) {
		return
	}
	// 等待发送游戏
	waitMillis := g.SendQuizAt.UnixNano()/1e6 - time.Now().UnixNano()/1e6
	log.Infof("[%v] - quiz game %v upcoming in %v seconds...", g.daemonID, g.GameID, waitMillis/1000)
	timer := time.NewTimer(time.Millisecond * time.Duration(waitMillis))
	select {
	case <-g.ctx.Done():
		log.Warnf("[%v] - quiz game %v terminated before started", g.daemonID, g.GameID)
		return
	case <-timer.C:
		log.Infof("[%v] - quiz game %v started", g.daemonID, g.GameID)
		// 发送游戏信息
		msg, err := session.ChannelMessageSendComplex(g.ChannelID, g.ToSendQuestionMessage())
		if err != nil {
			log.Error(errors.WrapAndReport(err, "send quiz game message"))
			return
		}
		g.QuestionMessageID = &msg.ID
		g.Status = database.DiscordQuizGameStatusInProgress
	}
	// 更新游戏开始
	if err := g.UpdateGameStarted(); err != nil {
		log.Error(err)
	}
}

func (g *quizGame) ongoing() {
	if !g.Status.Is(database.DiscordQuizGameStatusInProgress) {
		return
	}
	log.Infof("quiz game %v ongoing...", g.GameID)
	if g.QuestionMessageID == nil {
		log.Error(errors.ErrorfAndReport("game %v in progress but question message id not found", g.GameID))
		return
	}
	// 等待游戏结束
	until := g.SendQuizAt.Add(time.Second * time.Duration(g.TimeLimitSec))
	waitMillis := until.UnixNano()/1e6 - time.Now().UnixNano()/1e6
	timer := time.NewTimer(time.Millisecond * time.Duration(waitMillis))
	select {
	case <-g.ctx.Done():
		log.Warnf("quiz game %v terminated when in progress", g.GameID)
		return
	case <-timer.C:
		log.Infof("quiz game %v finished", g.GameID)
		// 尝试截止游戏参与
		g.Status = database.DiscordQuizGameStatusFinished
		if _, err := session.ChannelMessageEditComplex(g.ToQuestionMessageEdit()); err != nil {
			log.Error(errors.WrapfAndReport(err, "edit message to finish quiz game %v", g.GameID))
		}
		// 结算游戏参与信息
		if err := g.finalizeParticipateInfo(); err != nil {
			log.Error(err)
			return
		}
		answermsg, err := session.ChannelMessageSendComplex(g.ChannelID, g.ToSendAnswerMessage())
		if err != nil {
			log.Error(errors.WrapfAndReport(err, "send quiz game %v answer message", g.GameID))
		} else {
			g.AnswerMessageID = &answermsg.ID
		}
	}
	// 更新游戏结束
	if err := g.UpdateGameFinished(); err != nil {
		log.Error(err)
	}
}

func (g *quizGame) finalizeParticipateInfo() error {
	gameKey := fmt.Sprintf("%v%v", quizGameParticipantsKeyPrefix, g.GameID)
	participates, err := cache.Redis.HGetAll(g.ctx, gameKey).Result()
	if err != nil {
		return errors.WrapAndReport(err, "query quiz game participants cache")
	}
	if len(participates) == 0 {
		return nil
	}
	var (
		participants, winners database.JSONBArray
	)
	for discordID, participantInfo := range participates {
		participants = append(participants, discordID)
		arr := strings.Split(participantInfo, "&")
		if arr[0] == g.CorrectAnswerOptionCustomID {
			winners = append(winners, discordID)
		}
	}
	g.Participants = &participants
	g.Winners = &winners
	return nil
}

func (g *quizGame) participate(discordUserID, optionID string) (participated bool, err error) {
	// 校验游戏进度
	if g.Expired() {
		return false, nil
	}
	// 缓存用户参与选项、参与时间
	gameKey := fmt.Sprintf("%v%v", quizGameParticipantsKeyPrefix, g.GameID)
	participateInfo := fmt.Sprintf("%v&%v", optionID, time.Now().UnixNano()/1e6)
	err = cache.Redis.HSet(g.ctx, gameKey, discordUserID, participateInfo).Err()
	return true, errors.WrapAndReport(err, "cache game participants")
}

var (
	ErrorGameFinished          = errors.New("game finished")
	ErrorUnableToTerminateGame = errors.New("game cannot terminate")
)

func (g *quizGame) Terminate() error {
	switch g.Status {
	case database.DiscordQuizGameStatusNotStarted:
		g.cancelFunc()
		return nil
	case database.DiscordQuizGameStatusInProgress:
		if g.QuestionMessageID == nil {
			return ErrorUnableToTerminateGame
		}
		err := session.ChannelMessageDelete(g.ChannelID, *g.QuestionMessageID)
		if err != nil {
			log.Error(errors.WrapAndReport(err, "delete quiz game message"))
			return ErrorUnableToTerminateGame
		}
		g.cancelFunc()
		return nil
	default:
		return ErrorGameFinished
	}
}

func (g *quizGame) answerOptionFromOptionID(id string) string {
	idInt, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		log.Error(errors.WrapAndReport(err, "parse answer option index string"))
		return ""
	}
	return g.AnswerOptions[idInt-1].(string)
}

func (g *quizGame) ToQuestionMessageEdit() *discordgo.MessageEdit {
	// 编辑消息、移除消息的select menu组件，用于游戏结束时使用
	message := g.ToSendQuestionMessage()
	return &discordgo.MessageEdit{
		Embeds:     message.Embeds,
		ID:         *g.QuestionMessageID,
		Channel:    g.ChannelID,
		Components: []discordgo.MessageComponent{},
	}
}

func (g *quizGame) ToSendQuestionMessage() *discordgo.MessageSend {
	var options []discordgo.SelectMenuOption
	for i, ops := range g.AnswerOptions {
		seq := i + 1
		option := discordgo.SelectMenuOption{
			Label: ops.(string),
			Value: strconv.Itoa(seq),
			Emoji: discordgo.ComponentEmoji{
				Name: answerOptionEmojis[seq],
			},
		}
		options = append(options, option)
	}
	// 问题游戏的描述与答案的select menu组件，用于游戏开始时使用
	return &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{
			{
				Title:       "Let's Play The Quiz Game",
				Description: "**Question**\n" + g.QuestionDescription + "\n\n**Time Allowed: " + strconv.Itoa(g.TimeLimitSec) + " s**",
			},
		},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.SelectMenu{
						// Select menu, must have a customID, so we set it to this value.
						CustomID:    fmt.Sprintf("%v%v", discordQuizGameCustomIDPrefix, g.GameID),
						Placeholder: "Choose your answer here 👇",
						Options:     options,
					},
				},
			},
		},
	}
}

func (g *quizGame) ToSendAnswerMessage() *discordgo.MessageSend {
	// 引用提问消息、展示答题结果，用于游戏结束时使用
	m := &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{
			{
				Description: "**Correct Answer**\n" + g.CorrectAnswerOption + "\n\n**Participation Information**\nTotal " +
					strconv.Itoa(len(*g.Participants)) + " participants, including " + strconv.Itoa(len(*g.Winners)) + " winners.",
			},
		},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "Click this button to check your result.",
						Style:    discordgo.DangerButton,
						CustomID: fmt.Sprintf("%v%v", discordQuizGameCheckResultCustomIDPrefix, g.GameID),
					},
				},
			},
		},
	}
	if g.QuestionMessageID != nil {
		m.Reference = &discordgo.MessageReference{
			MessageID: *g.QuestionMessageID,
			ChannelID: g.ChannelID,
			GuildID:   g.GuildID,
		}
	}
	return m
}

func (g *quizGame) participationInfo(ctx context.Context, discordUserID string) (*participation, error) {
	gameKey := fmt.Sprintf("%v%v", quizGameParticipantsKeyPrefix, g.GameID)
	result, err := cache.Redis.HGet(ctx, gameKey, discordUserID).Result()
	var pp participation
	if errors.Is(err, redis.Nil) {
		return &pp, nil
	}
	if err != nil {
		return nil, errors.WrapAndReport(err, "query user quiz game participation")
	}
	arr := strings.Split(result, "&")
	pp.participated = true
	pp.chosenOptionID = arr[0]
	pp.participateTime = arr[1]
	i, err := strconv.ParseInt(pp.chosenOptionID, 10, 64)
	if err != nil {
		return nil, errors.WrapAndReport(err, "calc quiz game chosen option")
	}
	pp.chosenAnswer = g.AnswerOptions[i-1].(string)
	pp.correctAnswer = g.CorrectAnswerOption
	pp.win = pp.chosenAnswer == pp.correctAnswer
	return &pp, nil
}

type participation struct {
	participated    bool
	participateTime string
	chosenOptionID  string
	chosenAnswer    string
	correctAnswer   string
	win             bool
}

func (in *participation) ReplyContent() *string {
	if !in.participated {
		return pointStr("`Sorry, you did not participate the quiz this time. Remember to come next time! 🖖.`")
	}
	if in.win {
		return pointStr("`Congrats! You won the quiz! \U0001F973.`")
	}
	return pointStr("`Sorry, your choice seems not right. Better luck next time! 😢. \nWhat you've chosen: " + in.chosenAnswer + "\nThe correct answer: " + in.correctAnswer + "`")
}

var (
	answerOptionEmojis = map[int]string{
		1: "1️⃣",
		2: "2️⃣",
		3: "3️⃣",
		4: "4️⃣",
		5: "5️⃣",
		6: "6️⃣",
		7: "7️⃣",
		8: "8️⃣",
		9: "9️⃣",
	}
)

const (
	quizGameInfoCacheKeyPrefix           = "quiz_game:"
	quizGameLotteryWinnersCacheKeyPrefix = "quiz_game_lottery_winners:"
	// 后缀为游戏id, value为用户的答案选项
	quizGameParticipantsKeyPrefix                   = "quiz_game_participants:"
	discordQuizGameCustomIDPrefix                   = "quiz_game_"
	discordQuizGameCheckResultCustomIDPrefix        = "quiz_check_"
	discordQuizGameLotteryCheckResultCustomIDPrefix = "quiz_lottery_check_"
)
