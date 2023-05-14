package aws

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"moff.io/moff-social/internal/cache"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"strings"
	"time"
)

type QueueMessageHandler func(*types.Message) (deleteMsg bool, err error)

func (s *Clients) NewSQSWorker(ctx context.Context, queueURL string, handler QueueMessageHandler) {
	go s.blockingConsumeSQSMessages(ctx, queueURL, handler)
}

func (s *Clients) blockingConsumeSQSMessages(ctx context.Context, queueURL string, handler QueueMessageHandler) {
	// 获取队列名称
	idx := strings.LastIndex(queueURL, "/")
	queueName := queueURL[idx+1:]
	log.Infof("Blocking consume messages from queue %v...", queueName)
	defer log.Infof("Stopped to consume messages from queue %v...", queueName)
	for {
		msg, err := s.GetSingleMessageFromSQS(ctx, queueURL)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			log.Error(err)
			continue
		}
		if msg == nil {
			continue
		}
		// 尝试添加消息去重缓存，添加成功则表示新消息，否则按历史消息处理，直接从队列删除该消息
		cacheKey := fmt.Sprintf("%v_deduplication:%v", queueName, *msg.MessageId)
		set, err := cache.Redis.SetNX(ctx, cacheKey, 1, time.Hour*24*3).Result()
		if err != nil {
			log.Error(errors.WrapAndReport(err, "deduplicate notification queue message"))
			continue
		}
		if !set {
			// 默认当前是重复消息
			if err := s.DeleteSingleMessageFromSQS(ctx, queueURL, *msg.ReceiptHandle); err != nil {
				log.Error(err)
			}
			continue
		}

		// 处理消息
		deleteMsg, err := handler(msg)
		if err != nil {
			log.Error(err)
			if err := cache.Redis.Del(ctx, cacheKey).Err(); err != nil {
				log.Error(errors.WrapfAndReport(err, "delete queue %v message %v deduplication", queueName, *msg.MessageId))
			}
			continue
		}
		if deleteMsg {
			// 删除消息
			if err := s.DeleteSingleMessageFromSQS(ctx, queueURL, *msg.ReceiptHandle); err != nil {
				log.Error(err)
			}
		} else {
			// 移除消息去重
			if err := cache.Redis.Del(ctx, cacheKey).Err(); err != nil {
				log.Error(errors.WrapfAndReport(err, "delete queue %v message %v deduplication", queueName, *msg.MessageId))
			}
		}
	}
}

func (s *Clients) GetSingleMessageFromSQS(ctx context.Context, queueUrl string) (*types.Message, error) {
	output, err := s.sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(queueUrl),
		MaxNumberOfMessages: 1,
	})
	if err != nil {
		return nil, errors.WrapfAndReport(err, "query sqs message from %s", queueUrl)
	}
	if len(output.Messages) == 0 {
		return nil, nil
	}
	return &output.Messages[0], nil
}

func (s *Clients) DeleteSingleMessageFromSQS(ctx context.Context, queueUrl, receiptHandle string) error {
	_, err := s.sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(queueUrl),
		ReceiptHandle: aws.String(receiptHandle),
	})
	return errors.WrapfAndReport(err, "delete sqs message from %s", queueUrl)
}

func (s *Clients) MultiTrySendMessageToSQS(ctx context.Context, queueUrl, message string, maxTry int) error {
	for i := 0; i < maxTry; i++ {
		_, err := s.sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:    aws.String(queueUrl),
			MessageBody: aws.String(message),
		})
		if err != nil {
			log.Error(errors.WrapfAndReport(err, "send sqs message to %s", queueUrl))
			continue
		}
		return nil
	}
	return errors.ErrorfAndReport("send sqs message to %s max try exceeded", queueUrl)
}

func (s *Clients) SendMessageToSQS(ctx context.Context, queueUrl, message string) error {
	_, err := s.sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueUrl),
		MessageBody: aws.String(message),
	})
	return errors.WrapfAndReport(err, "send sqs message to %s", queueUrl)
}
