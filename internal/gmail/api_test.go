package gmail

import (
	"context"
	"encoding/base64"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/api/gmail/v1"
)

func TestCreateMessage_NoAttachments(t *testing.T) {
	msg := createMessage("user@example.com", "Hello", "Body text", nil)
	if msg == nil || msg.Raw == "" {
		t.Fatal("expected non-empty message")
	}

	decoded, err := base64.URLEncoding.DecodeString(msg.Raw)
	if err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	str := string(decoded)

	if !strings.Contains(str, "Content-Type: text/plain") {
		t.Errorf("expected text/plain content type, got:\n%s", str)
	}
	if !strings.Contains(str, "To: user@example.com") {
		t.Errorf("missing To header")
	}
	if strings.Contains(str, "multipart/mixed") {
		t.Errorf("should not be multipart when no attachments")
	}
}

func TestCreateMessage_WithAttachments(t *testing.T) {
	attachments := []Attachment{
		{Filename: "doc.pdf", MimeType: "application/pdf", Data: []byte("fake-pdf-content")},
		{Filename: "note.txt", MimeType: "text/plain", Data: []byte("hello")},
	}

	msg := createMessage("user@example.com", "With file", "See attached", attachments)
	if msg == nil || msg.Raw == "" {
		t.Fatal("expected non-empty message")
	}

	decoded, err := base64.URLEncoding.DecodeString(msg.Raw)
	if err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	str := string(decoded)

	if !strings.Contains(str, "multipart/mixed") {
		t.Errorf("expected multipart/mixed, got:\n%s", str)
	}

	if !strings.Contains(str, `filename="doc.pdf"`) {
		t.Errorf("missing first attachment filename")
	}
	if !strings.Contains(str, `filename="note.txt"`) {
		t.Errorf("missing second attachment filename")
	}

	// Verify base64-encoded attachment data is present
	encPDF := base64.StdEncoding.EncodeToString([]byte("fake-pdf-content"))
	if !strings.Contains(str, encPDF) {
		t.Errorf("missing base64-encoded PDF data")
	}

	// Verify the body text is in a text/plain part
	if !strings.Contains(str, "See attached") {
		t.Errorf("missing body text in multipart message")
	}

	// Verify closing boundary
	if !strings.Contains(str, "--pi-google-") {
		t.Errorf("missing boundary markers")
	}
}

func TestCreateMessage_AttachmentDefaultMimeType(t *testing.T) {
	attachments := []Attachment{
		{Filename: "unknown.bin", Data: []byte("data")},
	}

	msg := createMessage("a@b.com", "S", "B", attachments)
	decoded, _ := base64.URLEncoding.DecodeString(msg.Raw)
	str := string(decoded)

	if !strings.Contains(str, "application/octet-stream") {
		t.Errorf("expected default mime type for attachment without MimeType")
	}
}

func TestEscapeFilename(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"normal.pdf", "normal.pdf"},
		{`file"name.pdf`, "file'name.pdf"},
		{"line\r\nbreak", "linebreak"},
	}
	for _, tt := range tests {
		got := escapeFilename(tt.input)
		if got != tt.expect {
			t.Errorf("escapeFilename(%q) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}

// --- pagination tests ---

type listCall struct {
	query      string
	maxResults int64
	pageToken  string
	labelIDs   []string
}

// fakeLister scripts paginated responses for the messagesLister interface.
type fakeLister struct {
	calls   []listCall
	pages   []*gmail.ListMessagesResponse
	msgByID map[string]*gmail.Message
	errIDs  map[string]bool
}

func (f *fakeLister) listMessages(_ context.Context, query string, maxResults int64, pageToken string, labelIDs []string) (*gmail.ListMessagesResponse, error) {
	f.calls = append(f.calls, listCall{
		query:      query,
		maxResults: maxResults,
		pageToken:  pageToken,
		labelIDs:   append([]string{}, labelIDs...),
	})
	idx := len(f.calls) - 1
	if idx < len(f.pages) {
		return f.pages[idx], nil
	}
	return &gmail.ListMessagesResponse{}, nil
}

func (f *fakeLister) getMessageMetadata(_ context.Context, id string) (*gmail.Message, error) {
	if f.errIDs[id] {
		return nil, fmt.Errorf("boom")
	}
	if m, ok := f.msgByID[id]; ok {
		return m, nil
	}
	return &gmail.Message{Id: id, Payload: &gmail.MessagePart{}}, nil
}

func testMsg(id, subject, from string) *gmail.Message {
	return &gmail.Message{
		Id: id,
		Payload: &gmail.MessagePart{
			Headers: []*gmail.MessagePartHeader{
				{Name: "Subject", Value: subject},
				{Name: "From", Value: from},
			},
		},
	}
}

// Regression test for the truncation bug: SearchEmails must NOT restrict
// results to the INBOX label, otherwise queries like "in:sent", "to:..." or
// "from:... OR from:..." return empty results.
func TestSearchEmails_NoInboxLabel(t *testing.T) {
	fl := &fakeLister{
		pages: []*gmail.ListMessagesResponse{
			{Messages: []*gmail.Message{testMsg("m1", "S1", "a@x.com")}, ResultSizeEstimate: 1},
		},
	}
	s := &Service{lister: fl}

	res, err := s.SearchEmails(context.Background(), "in:sent after:2026/08/01", 20, "")
	if err != nil {
		t.Fatalf("SearchEmails: %v", err)
	}
	if len(fl.calls) != 1 {
		t.Fatalf("expected 1 list call, got %d", len(fl.calls))
	}
	if len(fl.calls[0].labelIDs) != 0 {
		t.Errorf("SearchEmails passed labelIDs=%v, want none (must not restrict to INBOX)", fl.calls[0].labelIDs)
	}
	if fl.calls[0].query != "in:sent after:2026/08/01" {
		t.Errorf("query = %q, want %q", fl.calls[0].query, "in:sent after:2026/08/01")
	}
	if len(res.Messages) != 1 {
		t.Errorf("got %d messages, want 1", len(res.Messages))
	}
}

func TestListInbox_UsesInboxLabel(t *testing.T) {
	fl := &fakeLister{
		pages: []*gmail.ListMessagesResponse{
			{Messages: []*gmail.Message{testMsg("m1", "S1", "a@x.com")}},
		},
	}
	s := &Service{lister: fl}

	if _, err := s.ListInbox(context.Background(), 20, "", ""); err != nil {
		t.Fatalf("ListInbox: %v", err)
	}
	if len(fl.calls) != 1 {
		t.Fatalf("expected 1 list call, got %d", len(fl.calls))
	}
	if !reflect.DeepEqual(fl.calls[0].labelIDs, []string{"INBOX"}) {
		t.Errorf("ListInbox labelIDs = %v, want [INBOX]", fl.calls[0].labelIDs)
	}
}

func TestSearchEmails_Paginates(t *testing.T) {
	fl := &fakeLister{
		pages: []*gmail.ListMessagesResponse{
			{
				Messages:           []*gmail.Message{testMsg("m1", "S1", "a@x.com"), testMsg("m2", "S2", "b@x.com")},
				NextPageToken:      "tok-2",
				ResultSizeEstimate: 4,
			},
			{
				Messages: []*gmail.Message{testMsg("m3", "S3", "c@x.com"), testMsg("m4", "S4", "d@x.com")},
			},
		},
	}
	s := &Service{lister: fl}

	res, err := s.SearchEmails(context.Background(), "from:someone", 4, "")
	if err != nil {
		t.Fatalf("SearchEmails: %v", err)
	}
	if len(res.Messages) != 4 {
		t.Errorf("got %d messages, want 4", len(res.Messages))
	}
	if res.NextPageToken != "" {
		t.Errorf("nextPageToken = %q, want empty (mailbox exhausted)", res.NextPageToken)
	}
	if res.ResultEstimate != 4 {
		t.Errorf("resultEstimate = %d, want 4", res.ResultEstimate)
	}
	if len(fl.calls) != 2 {
		t.Fatalf("expected 2 list calls, got %d", len(fl.calls))
	}
	if fl.calls[0].maxResults != 4 || fl.calls[0].pageToken != "" {
		t.Errorf("first call = maxResults %d, pageToken %q; want 4, empty", fl.calls[0].maxResults, fl.calls[0].pageToken)
	}
	if fl.calls[1].maxResults != 2 || fl.calls[1].pageToken != "tok-2" {
		t.Errorf("second call = maxResults %d, pageToken %q; want 2, tok-2", fl.calls[1].maxResults, fl.calls[1].pageToken)
	}
}

func TestSearchEmails_StopsAtMaxResults(t *testing.T) {
	fl := &fakeLister{
		pages: []*gmail.ListMessagesResponse{
			{
				Messages:      []*gmail.Message{testMsg("m1", "S1", "a@x.com"), testMsg("m2", "S2", "b@x.com"), testMsg("m3", "S3", "c@x.com")},
				NextPageToken: "tok-2",
			},
			{
				Messages:      []*gmail.Message{testMsg("m4", "S4", "d@x.com"), testMsg("m5", "S5", "e@x.com")},
				NextPageToken: "tok-3",
			},
		},
	}
	s := &Service{lister: fl}

	res, err := s.SearchEmails(context.Background(), "x", 5, "")
	if err != nil {
		t.Fatalf("SearchEmails: %v", err)
	}
	if len(res.Messages) != 5 {
		t.Errorf("got %d messages, want 5", len(res.Messages))
	}
	// maxResults reached before the mailbox was exhausted: the token must be
	// kept so the caller can page on.
	if res.NextPageToken != "tok-3" {
		t.Errorf("nextPageToken = %q, want tok-3 (more results exist)", res.NextPageToken)
	}
}

func TestSearchEmails_ResumesFromPageToken(t *testing.T) {
	fl := &fakeLister{
		pages: []*gmail.ListMessagesResponse{
			{Messages: []*gmail.Message{testMsg("m10", "S10", "a@x.com")}, NextPageToken: "tok-next"},
		},
	}
	s := &Service{lister: fl}

	res, err := s.SearchEmails(context.Background(), "x", 1, "tok-start")
	if err != nil {
		t.Fatalf("SearchEmails: %v", err)
	}
	if len(fl.calls) != 1 || fl.calls[0].pageToken != "tok-start" {
		t.Fatalf("list calls = %+v, want single call with pageToken tok-start", fl.calls)
	}
	if res.NextPageToken != "tok-next" {
		t.Errorf("nextPageToken = %q, want tok-next", res.NextPageToken)
	}
}

func TestSearchEmails_Empty(t *testing.T) {
	fl := &fakeLister{pages: []*gmail.ListMessagesResponse{{}}}
	s := &Service{lister: fl}

	res, err := s.SearchEmails(context.Background(), "in:sent after:2026/09/01", 20, "")
	if err != nil {
		t.Fatalf("SearchEmails: %v", err)
	}
	if len(res.Messages) != 0 {
		t.Errorf("got %d messages, want 0", len(res.Messages))
	}
	if res.NextPageToken != "" {
		t.Errorf("nextPageToken = %q, want empty", res.NextPageToken)
	}
}

func TestSearchEmails_SkipsUnreadable(t *testing.T) {
	fl := &fakeLister{
		pages: []*gmail.ListMessagesResponse{
			{Messages: []*gmail.Message{testMsg("ok", "S", "a@x.com"), {Id: "bad"}}},
		},
		errIDs: map[string]bool{"bad": true},
	}
	s := &Service{lister: fl}

	res, err := s.SearchEmails(context.Background(), "x", 20, "")
	if err != nil {
		t.Fatalf("SearchEmails: %v", err)
	}
	if len(res.Messages) != 1 || res.Messages[0].ID != "ok" {
		t.Errorf("got messages %+v, want only the readable one", res.Messages)
	}
}

func TestSearchEmails_DefaultsMaxResults(t *testing.T) {
	fl := &fakeLister{
		pages: []*gmail.ListMessagesResponse{
			{Messages: []*gmail.Message{testMsg("m1", "S1", "a@x.com")}},
		},
	}
	s := &Service{lister: fl}

	if _, err := s.SearchEmails(context.Background(), "x", 0, ""); err != nil {
		t.Fatalf("SearchEmails: %v", err)
	}
	if len(fl.calls) != 1 || fl.calls[0].maxResults != 20 {
		t.Errorf("maxResults = %d, want default 20", fl.calls[0].maxResults)
	}
}
