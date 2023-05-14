package databus

import (
	"fmt"
	"gopkg.in/Shopify/sarama.v1"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"strings"
	// "gopkg.in/confluentinc/confluent-kafka-go.v1/kafka"
)

type Event interface {
	Serialize() []byte
	Topic() string
}

type DataBus struct {
	producer sarama.SyncProducer
}

var producer *DataBus

func InitDataBus(host string) {
	hosts := strings.Split(host, ",")
	conf := sarama.NewConfig()
	conf.Producer.Return.Successes = true
	if p, err := sarama.NewSyncProducer(hosts, conf); err != nil {
		log.Fatalf("Failed to create producer: %s", err)
	} else {
		producer = &DataBus{producer: p}
	}
	log.Info("Kafka producer initialized...")
}

func GetDataBus() *DataBus {
	return producer
}

func (db *DataBus) PublishRaw(topic string, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	_, _, err := db.producer.SendMessage(&sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.StringEncoder(raw)})
	if err != nil {
		return errors.WrapAndReport(err, "produce message")
	} else {
		//log.Debugf("produce message success-partation: %d, offset: %d", partationNum, offset)
	}
	return nil
}

func (db *DataBus) Publish(e Event) (err error) {
	return db.PublishRaw(e.Topic(), e.Serialize())
}

func (db *DataBus) PublishLocal(e Event) (err error) {
	fmt.Printf(" topic: %s\n message: %s\n", e.Topic(), string(e.Serialize()))
	return
}
