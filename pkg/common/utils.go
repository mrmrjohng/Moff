package common

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/bwmarrin/snowflake"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"math/rand"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

//ErrFatalLog prints log and terminates progress when got an error.
func ErrFatalLog(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

//NewCutUUIDString returns uuid string that cut `-`.
func NewCutUUIDString() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}

func DecodeTimeInSnowflake(id string) *time.Time {
	snowflake.Epoch = 1420070400000
	sid, err := snowflake.ParseString(id)
	if err != nil {
		log.Errorf("parse snowflake id %v:%v", id, err)
		return nil
	}
	ms := sid.Time()
	t := time.Unix(0, ms*int64(time.Millisecond))
	return &t
}

//NewRandomNumberString returns n digits number string.
func NewRandomNumberString(digit int) string {
	var nInt string
	if digit <= 0 {
		return nInt
	}
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < digit; i++ {
		intn := rand.Intn(10)
		nInt += strconv.Itoa(intn)
	}
	return nInt
}

func DoubleSHA256AndBase64(src []byte) string {
	takeSHA256 := sha256.Sum256
	hash := takeSHA256(src)
	hash = takeSHA256(hash[:])
	return base64.StdEncoding.EncodeToString(hash[:])
}

//GenerateMultiple64Bytes generates bytes that are multiples of 64 from given multiple number.
func GenerateMultiple64Bytes(multiple int) []byte {
	if multiple <= 0 {
		multiple = 1
	}
	list := make([]byte, 0, multiple*64)
	for i := 0; i < multiple; i++ {
		b := SHA512([]byte(uuid.New().String()))
		list = append(list, b[:]...)
	}
	return list
}

//MD5 returns the MD5 hash.
func MD5(data []byte) []byte {
	sum := md5.Sum(data)
	return sum[:]
}

//SHA256 returns the SHA256 hash.
func SHA256(buf []byte) []byte {
	h := sha256.New()
	h.Write(buf)
	return h.Sum(nil)
}

//SHA256HexString returns the hex string that encoded from SHA256 hash of source text.
func SHA256HexString(buf []byte) string {
	h := sha256.New()
	h.Write(buf)
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)
}

//SHA512 returns the SHA-512 hash.
func SHA512(buf []byte) []byte {
	h := sha512.New()
	h.Write(buf)
	return h.Sum(nil)
}

func MustGetJSONString(m interface{}) string {
	if m == nil {
		return "{}"
	}
	data, err := json.Marshal(m)
	if err != nil {
		log.Error(err)
		return "{}"
	}
	return string(data)
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

var src = rand.NewSource(time.Now().UnixNano())

// NewRandWordString 生成指定位数的字符串，字符串由大写与小写字母、阿拉伯数字构成
func NewRandWordString(n int) string {
	sb := strings.Builder{}
	sb.Grow(n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			sb.WriteByte(letterBytes[idx])
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return sb.String()
}

// SubChar 截取指定的字符段，[charFrom, CharTo)
func SubChar(str string, charFrom, CharTo int) string {
	if str == "" {
		return str
	}
	if charFrom < 0 {
		charFrom = 0
	}
	if charFrom >= CharTo {
		return ""
	}
	if CharTo >= CharCount(str) {
		return str
	}
	var charCount int
	var startB int
	var endB int
	var imageMinLen = 4
	for _, value := range str {
		_, size := utf8.DecodeRuneInString(string(value))
		if size < imageMinLen {
			charCount++
		} else {
			charCount += size - utf8.RuneCountInString(string(value))*2
		}
		if charCount > charFrom && charCount <= CharTo {
			endB += size
		} else if charCount <= charFrom {
			startB += size
			endB += size
		} else {
			break
		}
	}
	return str[startB:endB]
}

func CharCount(str string) int {
	var charCount int
	var imageMinLen = 4
	for _, value := range str {
		_, size := utf8.DecodeRuneInString(string(value))
		if size < imageMinLen {
			charCount++
			continue
		}
		charCount += size - utf8.RuneCountInString(string(value))*2
	}
	return charCount
}

func TrimIP(ip string) string {
	last := strings.LastIndex(ip, ":")
	if last != -1 {
		ip = ip[0:last]
	}
	return ip
}
