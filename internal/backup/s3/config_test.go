package s3

import "testing"

func TestParseURL(t *testing.T) {
	cases := []struct {
		in, bucket, prefix string
		ok                 bool
	}{
		{"s3://my-bucket/lossless", "my-bucket", "lossless", true},
		{"s3://my-bucket", "my-bucket", "", true},
		{"s3://my-bucket/", "my-bucket", "", true},
		{"s3://my-bucket/a/b/", "my-bucket", "a/b", true},
		{"https://my-bucket/x", "", "", false},
		{"s3:///x", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		b, p, err := ParseURL(c.in)
		if (err == nil) != c.ok || b != c.bucket || p != c.prefix {
			t.Fatalf("%q: bucket=%q prefix=%q err=%v", c.in, b, p, err)
		}
	}
}

func TestNormalizedRegionAndEndpoint(t *testing.T) {
	c, err := (Config{Bucket: "b"}).normalized()
	if err != nil || c.Region != "us-east-1" {
		t.Fatalf("default region: %+v err=%v", c, err)
	}
	c, err = (Config{Bucket: "b", Endpoint: "https://abc123.r2.cloudflarestorage.com", Region: "us-east-1"}).normalized()
	if err != nil || c.Region != "auto" {
		t.Fatalf("r2 must sign auto: %+v err=%v", c, err)
	}
	c, err = (Config{Bucket: "b", Endpoint: "https://abc123.eu.r2.cloudflarestorage.com"}).normalized()
	if err != nil || c.Region != "auto" {
		t.Fatalf("r2 jurisdiction must sign auto: %+v err=%v", c, err)
	}
	if _, err := (Config{Bucket: "b", Endpoint: "http://minio.example.com:9000"}).normalized(); err == nil {
		t.Fatal("http off loopback must be refused")
	}
	if _, err := (Config{Bucket: "b", Endpoint: "http://127.0.0.1:9000"}).normalized(); err != nil {
		t.Fatalf("loopback http allowed: %v", err)
	}
	if _, err := (Config{}).normalized(); err == nil {
		t.Fatal("empty bucket must be refused")
	}
}

func TestObjectURLAndFullKey(t *testing.T) {
	c := Config{Bucket: "b", Prefix: "p/q"}
	if got := c.fullKey("o/x"); got != "p/q/o/x" {
		t.Fatal(got)
	}
	if got := (Config{Bucket: "b"}).fullKey("manifest"); got != "manifest" {
		t.Fatal(got)
	}
	u, err := (Config{Bucket: "b", Region: "us-east-1"}).objectURL("o/x")
	if err != nil || u.String() != "https://b.s3.us-east-1.amazonaws.com/o/x" {
		t.Fatalf("virtual-host: %v %v", u, err)
	}
	u, err = (Config{Bucket: "b", Prefix: "p", Endpoint: "https://acct.r2.cloudflarestorage.com"}).objectURL("m/g 1")
	if err != nil || u.String() != "https://acct.r2.cloudflarestorage.com/b/p/m/g%201" {
		t.Fatalf("path-style: %v %v", u, err)
	}
}
