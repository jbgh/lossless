package s3

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSignMatchesAWSExample(t *testing.T) {
	cfg := Config{
		Bucket:    "examplebucket",
		Region:    "us-east-1",
		AccessKey: "AKIAIOSFODNN7EXAMPLE",
		SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}
	req, _ := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	now := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	sign(req, cfg, emptyPayloadHash, now)
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request," +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date," +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	if req.Header.Get("x-amz-date") != "20130524T000000Z" {
		t.Fatal(req.Header.Get("x-amz-date"))
	}
	if req.Header.Get("x-amz-content-sha256") != emptyPayloadHash {
		t.Fatal("payload hash header")
	}
}

func TestSignAddsSessionToken(t *testing.T) {
	cfg := Config{Bucket: "b", Region: "auto", AccessKey: "k", SecretKey: "s", SessionToken: "tok"}
	req, _ := http.NewRequest(http.MethodPut, "https://acct.r2.cloudflarestorage.com/b/x", nil)
	sign(req, cfg, emptyPayloadHash, time.Unix(0, 0).UTC())
	if req.Header.Get("x-amz-security-token") != "tok" {
		t.Fatal("token header missing")
	}
	auth := req.Header.Get("Authorization")
	if !strings.Contains(auth, "/auto/s3/aws4_request") || !strings.Contains(auth, "x-amz-security-token") {
		t.Fatal(auth)
	}
}
