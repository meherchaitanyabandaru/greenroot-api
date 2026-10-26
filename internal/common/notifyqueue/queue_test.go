package notifyqueue

import "testing"

func TestLinkAndDataJSON(t *testing.T) {
	if got := DataJSON(Link("quotation", 65)); got != `{"entity":"quotation","id":65}` {
		t.Fatalf("DataJSON(Link) = %s", got)
	}
	if DataJSON(nil) != "{}" {
		t.Fatal("no data is {}")
	}
}
