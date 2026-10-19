package email

import (
	"context"
	"errors"
	"fmt"
	"net/mail"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

const ProviderSES = "ses"

type emailSender interface {
	SendEmail(ctx context.Context, in *sesv2.SendEmailInput, opts ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error)
}

// SESProvider sends through Amazon SES. Region and credentials come from the
// standard AWS chain; the From domain must be verified in SES.
type SESProvider struct {
	client emailSender
}

func newSESProvider() (Provider, error) {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("EMAIL_PROVIDER=ses: load AWS config: %w", err)
	}
	if cfg.Region == "" {
		return nil, errors.New("EMAIL_PROVIDER=ses: AWS_REGION is not set (use ap-south-1 for India)")
	}
	return SESProvider{client: sesv2.NewFromConfig(cfg)}, nil
}

func (SESProvider) Name() string { return ProviderSES }
func (SESProvider) Live() bool   { return true }

func (p SESProvider) Send(ctx context.Context, msg Message) (Result, error) {
	body := &types.Body{}
	if msg.HTML != "" {
		body.Html = &types.Content{Data: aws.String(msg.HTML), Charset: aws.String("UTF-8")}
	}
	if msg.Text != "" {
		body.Text = &types.Content{Data: aws.String(msg.Text), Charset: aws.String("UTF-8")}
	}
	in := &sesv2.SendEmailInput{
		// mail.Address encodes non-ASCII display names (RFC 2047).
		FromEmailAddress: aws.String((&mail.Address{Name: msg.FromName, Address: msg.From}).String()),
		Destination:      &types.Destination{ToAddresses: []string{msg.To}},
		Content: &types.EmailContent{Simple: &types.Message{
			Subject: &types.Content{Data: aws.String(msg.Subject), Charset: aws.String("UTF-8")},
			Body:    body,
		}},
	}
	if msg.ReplyTo != "" {
		in.ReplyToAddresses = []string{msg.ReplyTo}
	}
	out, err := p.client.SendEmail(ctx, in)
	if err != nil {
		return Result{}, fmt.Errorf("ses send: %w", err)
	}
	return Result{ProviderMessageID: aws.ToString(out.MessageId)}, nil
}
