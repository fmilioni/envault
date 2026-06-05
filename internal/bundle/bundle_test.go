package bundle

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func samplePayload() Payload {
	return Payload{Projects: []Project{{
		Name: "app",
		Stages: []Stage{{
			Name:    "prod",
			SavedAt: time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC),
			Files:   []File{{Path: ".env", Content: []byte("SECRET=TOPSECRETVALUE\n")}},
		}},
	}}}
}

func assertSamePayload(t *testing.T, got *Payload) {
	t.Helper()
	want := samplePayload()
	if len(got.Projects) != 1 || got.Projects[0].Name != "app" {
		t.Fatalf("projects = %+v", got.Projects)
	}
	gs := got.Projects[0].Stages
	if len(gs) != 1 || gs[0].Name != "prod" {
		t.Fatalf("stages = %+v", gs)
	}
	if !gs[0].SavedAt.Equal(want.Projects[0].Stages[0].SavedAt) {
		t.Errorf("savedAt = %v", gs[0].SavedAt)
	}
	if len(gs[0].Files) != 1 || gs[0].Files[0].Path != ".env" ||
		!bytes.Equal(gs[0].Files[0].Content, want.Projects[0].Stages[0].Files[0].Content) {
		t.Errorf("files = %+v", gs[0].Files)
	}
}

func TestPlaintextRoundTrip(t *testing.T) {
	data, err := Build(samplePayload(), "", false)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// "plaintext" means not encrypted: the content is recoverable (base64), unlike
	// the encrypted case below.
	enc := base64.StdEncoding.EncodeToString([]byte("SECRET=TOPSECRETVALUE\n"))
	if !bytes.Contains(data, []byte(enc)) {
		t.Errorf("plaintext bundle should carry recoverable content")
	}

	b, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if b.IsEncrypted() {
		t.Fatal("plaintext bundle reported as encrypted")
	}
	got, err := b.Decode("")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	assertSamePayload(t, got)
}

func TestEncryptedRoundTrip(t *testing.T) {
	data, err := Build(samplePayload(), "hunter2", true)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Neither the raw secret nor its base64 may appear in the ciphertext bundle.
	if bytes.Contains(data, []byte("TOPSECRETVALUE")) {
		t.Error("encrypted bundle leaks raw secret")
	}
	enc := base64.StdEncoding.EncodeToString([]byte("SECRET=TOPSECRETVALUE\n"))
	if bytes.Contains(data, []byte(enc)) {
		t.Error("encrypted bundle leaks base64 content")
	}

	b, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !b.IsEncrypted() {
		t.Fatal("encrypted bundle reported as plaintext")
	}
	got, err := b.Decode("hunter2")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	assertSamePayload(t, got)
}

func TestWrongPasswordFails(t *testing.T) {
	data, _ := Build(samplePayload(), "right", true)
	b, _ := Parse(data)
	if _, err := b.Decode("wrong"); err == nil {
		t.Fatal("expected error decoding with wrong password")
	}
}

func TestEncryptRequiresPassword(t *testing.T) {
	if _, err := Build(samplePayload(), "", true); err == nil {
		t.Fatal("expected error encrypting without a password")
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte("not json")); err == nil {
		t.Fatal("expected parse error on garbage")
	}
}

func TestDecodeRejectsUnknownCipher(t *testing.T) {
	data, _ := Build(samplePayload(), "pw", true)
	b, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	b.env.Cipher = "ChaCha20" // a future cipher the header could declare
	if _, err := b.Decode("pw"); err == nil || !strings.Contains(err.Error(), "unsupported bundle cipher") {
		t.Fatalf("expected unsupported-cipher error, got %v", err)
	}
}
