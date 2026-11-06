// Package weread imports local ebooks into a WeRead (微信读书)
// account.
//
// WeRead publishes no upload API. The client identifies as the WeRead e-ink
// app (version 2.1.2): it logs in through WeChat's QR flow for that app,
// sends that app's version headers, and uploads through the same
// credential → Tencent COS → notify sequence the app uses. Those endpoints
// are unofficial and may change without notice. The client does not solve
// captchas or other verification challenges.
package weread

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"strings"
)

const (
	defaultAPIBase  = "https://i.weread.qq.com"
	defaultOpenBase = "https://open.weixin.qq.com"
	defaultLongBase = "https://long.open.weixin.qq.com"
	cosHostSuffix   = ".cos.accelerate.myqcloud.com"

	// WeChat open-platform app that the e-ink client logs in as.
	wechatAppID = "wxab9b71ad2b90ff34"
	wechatScope = "snsapi_userinfo,snsapi_timeline,snsapi_friend"
	qrNonce     = "weread"

	deviceName = "BOOX"
	deviceType = 3

	appVersion = "2.1.2.10245900"
	userAgent  = "WeRead/2.1.2 WRBrand/Onyx wr_eink Dalvik/2.1.0 (Linux; U; Android 11; BOOX Build/onyx)"

	deviceIDPrefix  = "eink334691225"
	installIDPrefix = "eink31"
)

// setDeviceHeaders adds the version headers the e-ink client sends on every
// WeRead request.
func setDeviceHeaders(h http.Header) {
	h.Set("User-Agent", userAgent)
	h.Set("baseapi", "30")
	h.Set("appver", appVersion)
	h.Set("basever", appVersion)
	h.Set("osver", "11")
	h.Set("channelId", "900")
}

// newDeviceID returns a device identifier in the e-ink client's format: a
// fixed prefix followed by a zero-padded 63-bit random number.
func newDeviceID() string {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 63))
	if err != nil {
		panic(fmt.Sprintf("weread: crypto/rand failed: %v", err))
	}
	return fmt.Sprintf("%s%019d", deviceIDPrefix, n.Uint64())
}

func newInstallID() string {
	return installIDPrefix + randomDigits(26)
}

func randomDigits(n int) string {
	var b strings.Builder
	for range n {
		b.WriteByte(byte('0' + randomInt(10)))
	}
	return b.String()
}

// randomInt returns a uniform random integer in [0, n).
func randomInt(n int64) int64 {
	v, err := rand.Int(rand.Reader, big.NewInt(n))
	if err != nil {
		panic(fmt.Sprintf("weread: crypto/rand failed: %v", err))
	}
	return v.Int64()
}

// loginSignature signs a /login request body.
func loginSignature(timestamp int64, deviceID string, random int64) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%d%s%d", timestamp, deviceID, random))
	return hex.EncodeToString(sum[:])
}
