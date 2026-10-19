package email

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
)

type fakeEmailSender struct{ in *sesv2.SendEmailInput }

func (f *fakeEmailSender) SendEmail(_ context.Context, in *sesv2.SendEmailInput, _ ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error) {
	f.in = in
	return &sesv2.SendEmailOutput{MessageId: aws.String("ses-1")}, nil
}

func TestSESProvider_SendMapsFields(t *testing.T) {
	fake := &fakeEmailSender{}
	res, err := SESProvider{client: fake}.Send(context.Background(), Message{
		To: "ravi@example.com", FromName: "GreenRoot", From: "no-reply@greenroot.in", ReplyTo: "help@greenroot.in",
		Subject: "Welcome", HTML: "<p>Hi</p>", Text: "Hi",
	})
	if err != nil || res.ProviderMessageID != "ses-1" {
		t.Fatalf("send: %v %+v", err, res)
	}
	in := fake.in
	if got := aws.ToString(in.FromEmailAddress); got != `"GreenRoot" <no-reply@greenroot.in>` {
		t.Fatalf("from = %q", got)
	}
	if in.Destination.ToAddresses[0] != "ravi@example.com" || in.ReplyToAddresses[0] != "help@greenroot.in" {
		t.Fatalf("addresses: %+v", in)
	}
	m := in.Content.Simple
	if aws.ToString(m.Subject.Data) != "Welcome" || aws.ToString(m.Body.Html.Data) != "<p>Hi</p>" || aws.ToString(m.Body.Text.Data) != "Hi" {
		t.Fatalf("content: %+v", m)
	}
}

func TestNew_SESRequiresRegion(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "none"))
	if _, err := New("ses"); err == nil {
		t.Fatal("ses provider must refuse to start without a region")
	}
}
