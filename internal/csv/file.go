package csv

import (
	"context"
	"encoding/csv"
	"fmt"
	"github.com/google/uuid"
	"moff.io/moff-social/internal/aws"
	"moff.io/moff-social/pkg/errors"
	"os"
)

func WriteCsvAndUploadToS3(s3ObjectKey string, records [][]string) error {
	filePath := fmt.Sprintf("/cache/%v.csv", uuid.New().String())
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return errors.WrapAndReport(err, "open temp file")
	}
	writer := csv.NewWriter(file)
	if err := writer.WriteAll(records); err != nil {
		return errors.WrapAndReport(err, "write csv to temp file")
	}
	writer.Flush()

	if err := file.Close(); err != nil {
		return errors.WrapAndReport(err, "close csv file")
	}

	file, err = os.Open(filePath)
	if err != nil {
		return errors.WrapAndReport(err, "open csv file")
	}
	err = aws.Client.PutFileToS3WithPublicRead(context.TODO(), "moff-public", s3ObjectKey, file)
	return errors.WrapAndReport(err, "upload csv to s3")
}
