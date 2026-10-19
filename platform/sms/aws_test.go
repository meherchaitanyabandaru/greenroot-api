package sms

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/pinpointsmsvoicev2"
	"github.com/aws/aws-sdk-go-v2/service/pinpointsmsvoicev2/types"
)

type fakeTextSender struct {
	in *pinpointsmsvoicev2.SendTextMessageInput
}

func (f *fakeTextSender) SendTextMessage(_ context.Context, in *pinpointsmsvoicev2.SendTextMessageInput, _ ...func(*pinpointsmsvoicev2.Options)) (*pinpointsmsvoicev2.SendTextMessageOutput, error) {
	f.in = in
	return &pinpointsmsvoicev2.SendTextMessageOutput{MessageId: aws.String("aws-1")}, nil
}

func TestAWSProvider_SendMapsDLTFields(t *testing.T) {
	fake := &fakeTextSender{}
	res, err := AWSProvider{client: fake}.Send(context.Background(), Message{
		To: "9876543210", Body: "Your OTP is 482913", Header: "GRROOT", PEID: "pe-1", TemplateID: "tpl-1",
	})
	if err != nil || res.ProviderMessageID != "aws-1" {
		t.Fatalf("send: %v %+v", err, res)
	}
	in := fake.in
	if aws.ToString(in.DestinationPhoneNumber) != "+919876543210" || aws.ToString(in.OriginationIdentity) != "GRROOT" ||
		in.MessageType != types.MessageTypeTransactional {
		t.Fatalf("wrong routing fields: %+v", in)
	}
	if in.DestinationCountryParameters["IN_ENTITY_ID"] != "pe-1" || in.DestinationCountryParameters["IN_TEMPLATE_ID"] != "tpl-1" {
		t.Fatalf("DLT params not passed: %v", in.DestinationCountryParameters)
	}
}

func TestNew_AWSRequiresRegion(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "none"))
	if _, err := New("aws"); err == nil {
		t.Fatal("aws provider must refuse to start without a region")
	}
	t.Setenv("AWS_REGION", "ap-south-1")
	if p, err := New("aws"); err != nil || !p.Live() {
		t.Fatalf("aws provider with region: %v", err)
	}
}
