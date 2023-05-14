package config

import (
	"flag"
	"fmt"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"time"
)

// DBCredential struct
type DBCredential struct {
	Address  string `yaml:"address"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Port     string `yaml:"port"`
	Database string `yaml:"database"`
}

func (c *DBCredential) Dsn() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s",
		c.Address, c.Port, c.User, c.Password, c.Database)
}

// Configuration struct
type Configuration struct {
	RedisCredential  DBCredential   `yaml:"redis"`
	Postgres         DBCredential   `yaml:"postgres"`
	AwsS3            aws            `yaml:"aws"`
	MoralisAPIKey    string         `yaml:"moralis_api_key"`
	DiscordBot       DiscordBot     `yaml:"discord_bot"`
	moffGuild        Guild          `yaml:"moff_guild"`
	MongodbURI       string         `yaml:"mongodb_uri"`
	LarkAlarmWebhook string         `yaml:"lark_alarm_webhook"`
	DiscordExpRule   DiscordExpRule `yaml:"discord_exp_rule"`
	Google           Google         `yaml:"google"`
	Twitter          Twitter        `yaml:"twitter"`
	KafkaServer      string         `yaml:"kafka-server"`
}

type DiscordExpRule struct {
	OnReaction          int `yaml:"on_reaction"`
	OnInteraction       int `yaml:"on_interaction"`
	OnTenCharMessage    int `yaml:"on_ten_char_message"`
	OnTwentyCharMessage int `yaml:"on_twenty_char_message"`
	OnThirtyCharMessage int `yaml:"on_thirty_char_message"`
}

type DiscordBot struct {
	AppID            string        `yaml:"app_id"`
	AuthToken        string        `yaml:"auth_token"`
	AppConnectionURL string        `yaml:"app_connection_url"`
	MessageQueues    MessageQueues `yaml:"message_queues"`
}

func (in DiscordBot) IsMe(appID string) bool {
	return in.AppID == appID
}

type MessageQueues struct {
	NotificationQueueURL               string `yaml:"notification_queue_url"`
	MemberExpQueueURL                  string `yaml:"member_exp_queue_url"`
	GenerateCommunityQuestRewardsQueue string `yaml:"generate_community_quest_rewards_queue"`
}

type Google struct {
	Credential  GoogleCredential `yaml:"credential"`
	Oauth2Token Oauth2Token      `yaml:"oauth2_token"`
}

type GoogleCredential struct {
	ClientID     string   `yaml:"client_id"`
	ClientSecret string   `yaml:"client_secret"`
	RedirectURIs []string `yaml:"redirect_uris"`
	AuthURI      string   `yaml:"auth_uri"`
	TokenURI     string   `yaml:"token_uri"`
}

type Oauth2Token struct {
	AccessToken  string    `yaml:"access_token"`
	TokenType    string    `yaml:"token_type"`
	RefreshToken string    `yaml:"refresh_token"`
	Expiry       time.Time `yaml:"expiry"`
}

type Guild struct {
	ID                      string          `yaml:"id"`
	AuthorizedGuilds        []string        `yaml:"authorized_guilds"`
	AuthorizedGuildsMapping map[string]bool `yaml:"-"`
	Unbelievaboat           Unbelievaboat   `yaml:"unbelievaboat"`
}

func (in *Guild) initAuthorizedGuildsMapping() {
	in.AuthorizedGuildsMapping = make(map[string]bool)
	for _, gid := range in.AuthorizedGuilds {
		in.AuthorizedGuildsMapping[gid] = true
	}
}

type Unbelievaboat struct {
	RefreshIntervalMin int               `yaml:"refresh_interval_min"`
	AuthToken          string            `yaml:"auth_token"`
	MonthlySettlement  MonthlySettlement `yaml:"monthly_settlement"`
}

type MonthlySettlement struct {
	Enabled              bool `yaml:"enabled"`
	MaxDragonball        int  `yaml:"max_dragonball"`
	BalancePerDragonball int  `yaml:"balance_per_dragonball"`
}

type Twitter struct {
	ApiURL          string `yaml:"api_url"`
	ApiKey          string `yaml:"api_key"`
	ApiSecret       string `yaml:"api_secret"`
	RefreshTokenURL string `yaml:"refresh_token_url"`
}

// aws conf
type aws struct {
	Credential awsCredential `yaml:"credential"`
	Bucket     awsBucket     `yaml:"bucket"`
}

type awsCredential struct {
	Key    string `yaml:"key"`
	Secret string `yaml:"secret"`
}

type awsBucket struct {
	Name   string `yaml:"name"`
	Region string `yaml:"region"`
}

// GetRedisAddress prints redis credential info.
func (c *DBCredential) GetRedisAddress() string {
	return fmt.Sprintf("%v:%v", c.Address, c.Port)
}

func readConfig(path string) (Configuration, error) {
	logrus.Info("Starting to load configuration file ...")
	dat, err := ioutil.ReadFile(path)
	if err != nil {
		logrus.Fatal(err)
	}
	t := Configuration{}
	err = yaml.Unmarshal(dat, &t)

	if err != nil {
		if os.IsNotExist(err) {
			logrus.Fatalf("file %s does not exist", path)
		} else {
			logrus.Fatalf("fail to decode config error: %v", err)
		}
	}
	return t, nil
}

var Global *Configuration

// Read reads configuration information from yml.
func Read() {
	configFilePath := flag.String("config-path", "internal/config/config.yml", "The path to the configuration file")
	flag.Parse()
	logrus.Infof("Loading configuration file from %s", *configFilePath)
	globalConfig, err := readConfig(*configFilePath)
	if err != nil {
		logrus.Fatal(err)
	}
	globalConfig.moffGuild.initAuthorizedGuildsMapping()
	Global = &globalConfig
}
