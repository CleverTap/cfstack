package session

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
)

type Opts struct {
	Profile string
	Region  string
}

func NewSession(opts *Opts) (*session.Session, error) {
	if opts.Profile == "" {
		opts.Profile = "default"
	}
	return session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			CredentialsChainVerboseErrors: aws.Bool(true),
			Region:                        &opts.Region,
		},
		SharedConfigState: session.SharedConfigEnable,
		Profile:           opts.Profile,
	})
}
