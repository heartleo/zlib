package weread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

var (
	// qrLifetime bounds the whole wait for a QR confirmation.
	qrLifetime = 5 * time.Minute
	// pollTimeout bounds one long-poll request; WeChat holds it open.
	pollTimeout = 65 * time.Second
	// pollDelay spaces polls after WeChat reports "still waiting".
	pollDelay = time.Second
)

// WeChat long-poll states (wx_errcode).
const (
	qrWaiting   = 408
	qrScanned   = 404
	qrConfirmed = 405
	qrExpired   = 402
	qrDeclined  = 403
)

// QRCode is a pending WeChat login. URL is the content to encode in the QR
// image the user scans with WeChat.
type QRCode struct {
	UUID string
	URL  string
}

// Ping checks that the WeRead API answers, without logging in. It requests
// the public QR ticket, the first step of a login.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	var ticket struct {
		Signature string `json:"signature"`
	}
	if err := c.getJSON(ctx, c.apiBase+"/wxticket?nonceStr="+qrNonce, setDeviceHeaders, &ticket); err != nil {
		return fmt.Errorf("WeRead is unreachable: %w", err)
	}
	if ticket.Signature == "" {
		return errors.New("WeRead answered without a login ticket")
	}
	return nil
}

// RequestQRCode starts a WeChat QR login for the WeRead e-ink app.
func (c *Client) RequestQRCode(ctx context.Context) (QRCode, error) {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	var ticket struct {
		Signature string      `json:"signature"`
		TimeStamp json.Number `json:"timeStamp"`
	}
	if err := c.getJSON(ctx, c.apiBase+"/wxticket?nonceStr="+qrNonce, setDeviceHeaders, &ticket); err != nil {
		return QRCode{}, fmt.Errorf("WeRead QR ticket: %w", err)
	}
	if ticket.Signature == "" || ticket.TimeStamp == "" {
		return QRCode{}, errors.New("WeRead QR ticket response is incomplete")
	}

	query := url.Values{
		"appid":     {wechatAppID},
		"noncestr":  {qrNonce},
		"timestamp": {ticket.TimeStamp.String()},
		"scope":     {wechatScope},
		"signature": {ticket.Signature},
	}
	var qr struct {
		ErrCode int    `json:"errcode"`
		UUID    string `json:"uuid"`
	}
	err := c.getJSON(ctx, c.openBase+"/connect/sdk/qrconnect?"+query.Encode(), func(h http.Header) {
		h.Set("User-Agent", userAgent)
	}, &qr)
	if err != nil {
		return QRCode{}, fmt.Errorf("WeChat QR request: %w", err)
	}
	if qr.ErrCode != 0 || qr.UUID == "" {
		return QRCode{}, fmt.Errorf("WeChat QR request failed (errcode %d)", qr.ErrCode)
	}
	return QRCode{
		UUID: qr.UUID,
		URL:  c.openBase + "/connect/confirm?uuid=" + url.QueryEscape(qr.UUID),
	}, nil
}

// WaitForConfirmation long-polls WeChat until the user confirms the login
// on their phone, then returns the authorization code for Exchange.
// onScanned, if set, runs once when WeChat reports the code was scanned.
func (c *Client) WaitForConfirmation(ctx context.Context, uuid string, onScanned func()) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, qrLifetime)
	defer cancel()
	scanned := false
	last := 0
	for {
		state, code, err := c.pollOnce(ctx, uuid, last)
		if err != nil {
			if ctx.Err() != nil {
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					return "", ErrQRExpired
				}
				return "", ctx.Err()
			}
			return "", err
		}
		switch state {
		case qrConfirmed:
			if code == "" {
				return "", errors.New("WeChat confirmed the login without a code")
			}
			return code, nil
		case qrScanned:
			if !scanned && onScanned != nil {
				onScanned()
			}
			scanned = true
		case qrWaiting:
			select {
			case <-ctx.Done():
				continue
			case <-time.After(pollDelay):
			}
		case qrExpired:
			return "", ErrQRExpired
		case qrDeclined:
			return "", ErrQRDeclined
		default:
			return "", fmt.Errorf("WeChat QR poll returned unknown state %d", state)
		}
		last = state
	}
}

// pollOnce issues one long-poll request. A request that times out while the
// overall wait continues counts as "still waiting".
func (c *Client) pollOnce(ctx context.Context, uuid string, last int) (int, string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()
	query := url.Values{"f": {"json"}, "uuid": {uuid}}
	if last != 0 {
		query.Set("last", fmt.Sprint(last))
	}
	var result struct {
		ErrCode int    `json:"wx_errcode"`
		Code    string `json:"wx_code"`
	}
	err := c.getJSON(reqCtx, c.longBase+"/connect/l/qrconnect?"+query.Encode(), func(h http.Header) {
		h.Set("User-Agent", "Mozilla/5.0")
	}, &result)
	if err != nil && ctx.Err() == nil && errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
		return qrWaiting, "", nil
	}
	return result.ErrCode, result.Code, err
}

// Exchange trades a WeChat authorization code for WeRead credentials and
// makes them the client's credentials. A device ID already set on the
// client is kept, so a re-login keeps the same device.
func (c *Client) Exchange(ctx context.Context, code string) (Credentials, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	deviceID := c.creds.DeviceID
	if deviceID == "" {
		deviceID = newDeviceID()
	}
	timestamp := time.Now().UnixMilli()
	random := randomInt(1000)
	body := map[string]any{
		"appFirstInstall": 1,
		"code":            code,
		"deviceId":        deviceID,
		"deviceName":      deviceName,
		"deviceType":      deviceType,
		"installId":       newInstallID(),
		"isAutoLogout":    0,
		"isFromQrcode":    1,
		"random":          random,
		"signature":       loginSignature(timestamp, deviceID, random),
		"timestamp":       timestamp,
		"trackId":         "",
	}
	var resp loginResponse
	if err := c.postLogin(ctx, body, &resp); err != nil {
		return Credentials{}, fmt.Errorf("WeRead login: %w", err)
	}
	creds := Credentials{
		VID:          resp.VID.String(),
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		DeviceID:     deviceID,
	}
	if !creds.Valid() {
		return Credentials{}, errors.New("WeRead login returned incomplete credentials")
	}
	c.creds = creds
	return creds, nil
}
