package s3

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"os"
)

type S3 struct {
	client s3iface.S3API
}

func New(sess *session.Session) S3 {
	return S3{
		client: s3.New(sess),
	}
}

type Opts struct {
	Bucket   string
	Filepath string
	Key      string
}

func (s *S3) UploadToS3(opts *Opts) error {
	file, err := os.Open(opts.Filepath)
	if err != nil {
		return err
	}

	defer file.Close()

	_, err = s.client.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(opts.Bucket),
		Key:    aws.String(opts.Key),
		Body:   file,
	})

	if err != nil {
		return err
	}

	return nil
}
