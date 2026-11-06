package weread

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// uploadTimeout bounds one COS upload; a var so tests can shorten it.
var uploadTimeout = 30 * time.Minute

// cosBucketRe is the Tencent COS bucket name shape (name-appid). Checking it
// keeps a server-supplied bucket from steering the upload to another host.
var cosBucketRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// cosErrorCodeRe extracts <Code> from a COS XML error body.
var cosErrorCodeRe = regexp.MustCompile(`<Code>([A-Za-z]{1,64})</Code>`)

type cosCredentials struct {
	TmpSecretID  string `json:"TmpSecretId"`
	TmpSecretKey string `json:"TmpSecretKey"`
	Token        string `json:"Token"`
}

func defaultCOSURL(bucket, key string) string {
	u := url.URL{Scheme: "https", Host: bucket + cosHostSuffix, Path: "/" + key}
	return u.String()
}

// putCOS uploads body to the credential's object with a single signed PUT. A
// single PUT accepts objects far above MaxImportSize, so no multipart path is
// needed.
func (c *Client) putCOS(ctx context.Context, cred uploadCredential, body io.Reader, size int64) error {
	bucket, key, creds, expires := cred.bucket, cred.key, cred.creds, cred.expires
	ctx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.cosURL(bucket, key), body)
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("x-cos-security-token", creds.Token)

	now := time.Now().Unix()
	start, end := now-60, now+3600
	if expires > now && expires < end {
		end = expires
	}
	req.Header.Set("Authorization", cosAuthorization(creds.TmpSecretID, creds.TmpSecretKey, http.MethodPut, "/"+key,
		map[string]string{"content-length": strconv.FormatInt(size, 10), "host": req.URL.Host}, start, end))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("COS upload: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	msg := fmt.Sprintf("COS upload failed (HTTP %d)", resp.StatusCode)
	if m := cosErrorCodeRe.FindSubmatch(raw); m != nil {
		msg += ": " + string(m[1])
	}
	return fmt.Errorf("%s", msg)
}

// cosAuthorization builds a Tencent COS XML API request signature
// (q-sign-algorithm=sha1). path is the unescaped object path; headers are the
// signed headers with lower-case names. No query parameters are signed.
func cosAuthorization(secretID, secretKey, method, path string, headers map[string]string, start, end int64) string {
	keyTime := fmt.Sprintf("%d;%d", start, end)
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	pairs := make([]string, len(names))
	for i, name := range names {
		pairs[i] = cosEscape(name) + "=" + cosEscape(headers[name])
	}
	httpString := strings.ToLower(method) + "\n" + path + "\n\n" + strings.Join(pairs, "&") + "\n"
	stringToSign := "sha1\n" + keyTime + "\n" + sha1Hex(httpString) + "\n"
	signKey := hmacSHA1Hex(secretKey, keyTime)
	signature := hmacSHA1Hex(signKey, stringToSign)
	return strings.Join([]string{
		"q-sign-algorithm=sha1",
		"q-ak=" + secretID,
		"q-sign-time=" + keyTime,
		"q-key-time=" + keyTime,
		"q-header-list=" + strings.Join(names, ";"),
		"q-url-param-list=",
		"q-signature=" + signature,
	}, "&")
}

// cosEscape is RFC 3986 percent-encoding, as COS signs it.
func cosEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func sha1Hex(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func hmacSHA1Hex(key, msg string) string {
	h := hmac.New(sha1.New, []byte(key))
	h.Write([]byte(msg))
	return hex.EncodeToString(h.Sum(nil))
}
