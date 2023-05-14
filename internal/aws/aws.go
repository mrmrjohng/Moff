package aws

import (
	"bytes"
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"io"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"time"
)

var (
	Client *Clients
)

func Init(bucketName, region string) {
	if bucketName == "" || region == "" {
		log.Fatalf("s3 bucket or region not present")
	}
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}
	Client = &Clients{
		bucketName: bucketName,
		region:     region,
		s3Client:   s3.NewFromConfig(cfg),
		ssmClient:  ssm.NewFromConfig(cfg),
		sqsClient:  sqs.NewFromConfig(cfg),
	}
}

type Clients struct {
	bucketName string
	region     string
	s3Client   *s3.Client
	ssmClient  *ssm.Client
	sqsClient  *sqs.Client
}

func (s *Clients) GetParameterFromSSM(ctx context.Context, paramName string) (*ssmtypes.Parameter, error) {
	input := &ssm.GetParameterInput{
		Name:           aws.String(paramName),
		WithDecryption: true,
	}
	parameter, err := s.ssmClient.GetParameter(ctx, input)
	if err != nil {
		return nil, errors.WrapAndReport(err, "query parameter from ssm")
	}
	return parameter.Parameter, nil
}

func (s *Clients) MustGetSSMParameter(ctx context.Context, paramName string) *ssmtypes.Parameter {
	input := &ssm.GetParameterInput{
		Name:           aws.String(paramName),
		WithDecryption: true,
	}
	parameter, err := s.ssmClient.GetParameter(ctx, input)
	if err != nil {
		panic(fmt.Sprintf("query parameter from s3:%v", err))
	}
	return parameter.Parameter
}

func (s *Clients) GetS3PresignedAccessURL(ctx context.Context, key string, expire time.Duration) (string, error) {
	request, err := s3.NewPresignClient(s.s3Client).PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expire))
	if err != nil {
		return "", errors.WithStackAndReport(err)
	}
	return request.URL, nil
}

func (s *Clients) PutFileToS3(ctx context.Context, key string, file io.Reader) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(key),
		Body:   file,
	}
	_, err := s.s3Client.PutObject(ctx, input)
	return errors.WrapAndReport(err, "put object to s3")
}

func (s *Clients) PutFileToS3WithPublicRead(ctx context.Context, bucket, key string, file io.Reader) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		ACL:    types.ObjectCannedACLPublicRead,
		Body:   file,
	}
	_, err := s.s3Client.PutObject(ctx, input)
	return errors.WrapAndReport(err, "put object to s3")
}

func (s *Clients) DeleteFileFromS3(ctx context.Context, key string) error {
	input := &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(key),
	}
	_, err := s.s3Client.DeleteObject(ctx, input)
	return errors.WrapAndReport(err, "delete s3 object")
}

const (
	httpsStr  = "https://"
	s3DotStr  = ".s3."
	amazonStr = ".amazonaws.com/"
)

func (s *Clients) PublicS3AccessURLFrom(key string) string {
	var buf bytes.Buffer
	buf.WriteString(httpsStr)
	buf.WriteString(s.bucketName)
	buf.WriteString(s3DotStr)
	buf.WriteString(s.region)
	buf.WriteString(amazonStr)
	buf.WriteString(key)
	return buf.String()
}
