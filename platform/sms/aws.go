package sms

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/pinpointsmsvoicev2"
	"github.com/aws/aws-sdk-go-v2/service/pinpointsmsvoicev2/types"
)

const ProviderAWS = "aws"

type textSender interface {
	SendTextMessage(ctx context.Context, in *pinpointsmsvoicev2.SendTextMessageInput, opts ...func(*pinpointsmsvoicev2.Options)) (*pinpointsmsvoicev2.SendTextMessageOutput, error)
}

// AWSProvider sends through AWS End User Messaging SMS. Region and
// credentials come from the standard AWS chain (AWS_REGION, IAM role or
// AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY); nothing AWS-specific is stored here.
type AWSProvider struct {
	client textSender
}

func newAWSProvider() (Provider, error) {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("SMS_PROVIDER=aws: load AWS config: %w", err)
	}
	if cfg.Region == "" {
		return nil, errors.New("SMS_PROVIDER=aws: AWS_REGION is not set (use ap-south-1 for India)")
	}
	return AWSProvider{client: pinpointsmsvoicev2.NewFromConfig(cfg)}, nil
}

func (AWSProvider) Name() string { return ProviderAWS }
func (AWSProvider) Live() bool   { return true }

// Send maps the DLT identifiers onto the India-specific parameters AWS
// forwards to the operator. The header is the AWS-registered sender ID.
func (p AWSProvider) Send(ctx context.Context, msg Message) (Result, error) {
	out, err := p.client.SendTextMessage(ctx, &pinpointsmsvoicev2.SendTextMessageInput{
		DestinationPhoneNumber: aws.String("+91" + msg.To),
		OriginationIdentity:    aws.String(msg.Header),
		MessageBody:            aws.String(msg.Body),
		MessageType:            types.MessageTypeTransactional,
		DestinationCountryParameters: map[string]string{
			string(types.DestinationCountryParameterKeyInEntityId):   msg.PEID,
			string(types.DestinationCountryParameterKeyInTemplateId): msg.TemplateID,
		},
	})
	if err != nil {
		return Result{}, fmt.Errorf("aws sms send: %w", err)
	}
	return Result{ProviderMessageID: aws.ToString(out.MessageId)}, nil
}
