package protocol

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"

	"filippo.io/edwards25519"
	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/nacl/box"
)

const (
	EnvelopeVersion = 1
	EnvelopeType    = "msg"
	NonceSize       = 24
	SeedSize        = ed25519.SeedSize
)

var (
	ErrInvalidSeed      = fmt.Errorf("protocol: Ed25519 seed must be %d bytes", SeedSize)
	ErrInvalidPublicKey = errors.New("protocol: Ed25519 public key must be 32 bytes")
	ErrInvalidEnvelope  = errors.New("protocol: unsupported envelope")
	ErrInvalidNonce     = fmt.Errorf("protocol: crypto_box nonce must be %d bytes", NonceSize)
)

// KeyPair is an Ed25519 identity. PrivateKey is the standard 64-byte Go form;
// only Seed() should be persisted.
type KeyPair struct {
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

// CurveKeyPair is the X25519 form libsodium derives from an Ed25519 identity.
type CurveKeyPair struct {
	PublicKey [32]byte
	SecretKey [32]byte
}

// Envelope is the protocol v1 encrypted message envelope.
type Envelope struct {
	V     int    `json:"v"`
	T     string `json:"t"`
	ID    string `json:"id"`
	From  string `json:"from"`
	To    string `json:"to"`
	TS    string `json:"ts"`
	Nonce string `json:"nonce"`
	CT    string `json:"ct"`
}

// PairingInvite is the payload encoded in a homing://pair QR code.
type PairingInvite struct {
	Server      string
	Token       string
	HostID      string
	Fingerprint string
}

// KeyPairFromSeed mirrors libsodium crypto_sign_seed_keypair.
func KeyPairFromSeed(seed []byte) (KeyPair, error) {
	if len(seed) != SeedSize {
		return KeyPair{}, ErrInvalidSeed
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	return KeyPair{
		PublicKey:  append(ed25519.PublicKey(nil), privateKey.Public().(ed25519.PublicKey)...),
		PrivateKey: privateKey,
	}, nil
}

// KeyPairFromSeedString is the vector-friendly form used by the JS/Android tests.
func KeyPairFromSeedString(seed string) (KeyPair, error) {
	return KeyPairFromSeed([]byte(seed))
}

func GenerateKeyPair() (KeyPair, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, err
	}
	return KeyPair{PublicKey: publicKey, PrivateKey: privateKey}, nil
}

func (k KeyPair) Seed() []byte {
	if len(k.PrivateKey) != ed25519.PrivateKeySize {
		return nil
	}
	return append([]byte(nil), k.PrivateKey.Seed()...)
}

// CurveKeys converts an Ed25519 identity to X25519 exactly as libsodium does.
func CurveKeys(keys KeyPair) (CurveKeyPair, error) {
	if len(keys.PublicKey) != ed25519.PublicKeySize {
		return CurveKeyPair{}, ErrInvalidPublicKey
	}
	publicPoint, err := new(edwards25519.Point).SetBytes(keys.PublicKey)
	if err != nil {
		return CurveKeyPair{}, fmt.Errorf("protocol: invalid Ed25519 public key: %w", err)
	}
	curvePublic := publicPoint.BytesMontgomery()
	if len(curvePublic) != 32 {
		return CurveKeyPair{}, errors.New("protocol: could not convert public key")
	}

	seed := keys.PrivateKey.Seed()
	digest := sha512.Sum512(seed)
	curveSecret := append([]byte(nil), digest[:32]...)
	curveSecret[0] &= 248
	curveSecret[31] &= 127
	curveSecret[31] |= 64

	var out CurveKeyPair
	copy(out.PublicKey[:], curvePublic)
	copy(out.SecretKey[:], curveSecret)
	return out, nil
}

// DeviceID is lowercase unpadded base32(BLAKE2b-128(public key)).
func DeviceID(publicKey []byte) string {
	digest := blake2bSum(16, publicKey)
	return base32Lower(digest)
}

// Fingerprint is lowercase hex(BLAKE2b-64(public key)).
func Fingerprint(publicKey []byte) string {
	return hex.EncodeToString(blake2bSum(8, publicKey))
}

func VerifyHostFingerprint(publicKey []byte, expected string) bool {
	return strings.EqualFold(Fingerprint(publicKey), strings.TrimSpace(expected))
}

// SASWords derives the four BIP39 words shown to both devices during pairing.
func SASWords(hostPublicKey, clientPublicKey []byte) ([4]string, error) {
	var out [4]string
	if len(hostPublicKey) != ed25519.PublicKeySize || len(clientPublicKey) != ed25519.PublicKeySize {
		return out, ErrInvalidPublicKey
	}
	material := make([]byte, 0, len(hostPublicKey)+len(clientPublicKey))
	material = append(material, hostPublicKey...)
	material = append(material, clientPublicKey...)
	digest := blake2bSum(8, material)

	bits := binary.BigEndian.Uint64(append([]byte{0, 0}, digest[:6]...))
	bits >>= 4
	for i := 0; i < 4; i++ {
		shift := uint((3 - i) * 11)
		index := (bits >> shift) & 0x7ff
		out[i] = bip39Words[index]
	}
	return out, nil
}

func HelloSignature(privateKey ed25519.PrivateKey, nonceBase64, timestamp string) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("protocol: invalid Ed25519 private key")
	}
	nonce, err := FromBase64(nonceBase64)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(privateKey, helloMaterial(nonce, timestamp)), nil
}

func VerifyHelloSignature(publicKey ed25519.PublicKey, nonceBase64, timestamp string, signature []byte) bool {
	nonce, err := FromBase64(nonceBase64)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(publicKey, helloMaterial(nonce, timestamp), signature)
}

func helloMaterial(nonce []byte, timestamp string) []byte {
	material := make([]byte, 0, len(nonce)+len(timestamp))
	material = append(material, nonce...)
	return append(material, timestamp...)
}

// CreateEnvelope encrypts payloadJSON as crypto_box_easy(payload, nonce,
// recipient X25519 public key, sender X25519 secret key).
func CreateEnvelope(
	from, to string,
	payloadJSON []byte,
	sender KeyPair,
	recipientPublicKey []byte,
) (Envelope, error) {
	return CreateEnvelopeWithNonce(
		from,
		to,
		payloadJSON,
		sender,
		recipientPublicKey,
		NewULID(),
		time.Now().UTC().Format(time.RFC3339Nano),
		nil,
	)
}

// CreateEnvelopeWithNonce is the deterministic form used by cross-language tests.
func CreateEnvelopeWithNonce(
	from, to string,
	payloadJSON []byte,
	sender KeyPair,
	recipientPublicKey []byte,
	id, timestamp string,
	nonce []byte,
) (Envelope, error) {
	if nonce == nil {
		nonce = make([]byte, NonceSize)
		if _, err := rand.Read(nonce); err != nil {
			return Envelope{}, err
		}
	}
	if len(nonce) != NonceSize {
		return Envelope{}, ErrInvalidNonce
	}
	ciphertext, err := SealBox(payloadJSON, nonce, sender, recipientPublicKey)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{
		V:     EnvelopeVersion,
		T:     EnvelopeType,
		ID:    id,
		From:  from,
		To:    to,
		TS:    timestamp,
		Nonce: ToBase64(nonce),
		CT:    ToBase64(ciphertext),
	}, nil
}

func SealBox(plaintext, nonce []byte, sender KeyPair, recipientPublicKey []byte) ([]byte, error) {
	if len(nonce) != NonceSize {
		return nil, ErrInvalidNonce
	}
	if len(recipientPublicKey) != ed25519.PublicKeySize {
		return nil, ErrInvalidPublicKey
	}
	senderCurve, err := CurveKeys(sender)
	if err != nil {
		return nil, err
	}
	recipientCurve, err := ed25519PublicToCurve(recipientPublicKey)
	if err != nil {
		return nil, err
	}
	var nonceArray [NonceSize]byte
	copy(nonceArray[:], nonce)
	return box.Seal(nil, plaintext, &nonceArray, &recipientCurve, &senderCurve.SecretKey), nil
}

// OpenEnvelope decrypts an envelope and returns the raw payload JSON.
func OpenEnvelope(envelope Envelope, recipient KeyPair, senderPublicKey []byte) ([]byte, error) {
	if envelope.V != EnvelopeVersion || envelope.T != EnvelopeType {
		return nil, ErrInvalidEnvelope
	}
	nonce, err := FromBase64(envelope.Nonce)
	if err != nil {
		return nil, fmt.Errorf("protocol: invalid nonce: %w", err)
	}
	ciphertext, err := FromBase64(envelope.CT)
	if err != nil {
		return nil, fmt.Errorf("protocol: invalid ciphertext: %w", err)
	}
	return OpenBox(ciphertext, nonce, recipient, senderPublicKey)
}

func OpenBox(ciphertext, nonce []byte, recipient KeyPair, senderPublicKey []byte) ([]byte, error) {
	if len(nonce) != NonceSize {
		return nil, ErrInvalidNonce
	}
	if len(ciphertext) < box.Overhead {
		return nil, errors.New("protocol: ciphertext is too short")
	}
	if len(senderPublicKey) != ed25519.PublicKeySize {
		return nil, ErrInvalidPublicKey
	}
	recipientCurve, err := CurveKeys(recipient)
	if err != nil {
		return nil, err
	}
	senderCurve, err := ed25519PublicToCurve(senderPublicKey)
	if err != nil {
		return nil, err
	}
	var nonceArray [NonceSize]byte
	copy(nonceArray[:], nonce)
	plaintext, ok := box.Open(nil, ciphertext, &nonceArray, &senderCurve, &recipientCurve.SecretKey)
	if !ok {
		return nil, errors.New("protocol: could not decrypt envelope")
	}
	return plaintext, nil
}

func ed25519PublicToCurve(publicKey []byte) ([32]byte, error) {
	var out [32]byte
	if len(publicKey) != ed25519.PublicKeySize {
		return out, ErrInvalidPublicKey
	}
	point, err := new(edwards25519.Point).SetBytes(publicKey)
	if err != nil {
		return out, fmt.Errorf("protocol: invalid Ed25519 public key: %w", err)
	}
	copy(out[:], point.BytesMontgomery())
	return out, nil
}

func RandomBytes(size int) ([]byte, error) {
	out := make([]byte, size)
	_, err := rand.Read(out)
	return out, err
}

func RandomPairToken() (string, error) {
	raw, err := RandomBytes(16)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func RandomCode6() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

func NewULID() string {
	return ulid.Make().String()
}

func ToBase64(value []byte) string {
	return base64.StdEncoding.EncodeToString(value)
}

func FromBase64(value string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(value)
}

func PairURI(invite PairingInvite) string {
	values := url.Values{}
	values.Set("s", invite.Server)
	values.Set("t", invite.Token)
	values.Set("h", invite.HostID)
	values.Set("fp", invite.Fingerprint)
	return "homing://pair?" + values.Encode()
}

func ParsePairURI(value string) (PairingInvite, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return PairingInvite{}, fmt.Errorf("protocol: invalid pairing URI: %w", err)
	}
	if parsed.Scheme != "homing" || parsed.Host != "pair" {
		return PairingInvite{}, errors.New("protocol: not a Homing pairing URI")
	}
	query := parsed.Query()
	invite := PairingInvite{
		Server:      query.Get("s"),
		Token:       query.Get("t"),
		HostID:      query.Get("h"),
		Fingerprint: query.Get("fp"),
	}
	if invite.Server == "" || invite.Token == "" || invite.HostID == "" || invite.Fingerprint == "" {
		return PairingInvite{}, errors.New("protocol: pairing URI is incomplete")
	}
	return invite, nil
}

func NormalizeWebSocketURL(server string) (string, error) {
	raw := strings.TrimSpace(server)
	if raw == "" {
		return "", errors.New("protocol: server address is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "wss://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("protocol: invalid server address: %w", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https", "wss":
		parsed.Scheme = "wss"
	case "http", "ws":
		parsed.Scheme = "ws"
	default:
		return "", errors.New("protocol: server address must use ws, wss, http or https")
	}
	if parsed.Host == "" {
		return "", errors.New("protocol: server address has no host")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/v1/ws"
	}
	return parsed.String(), nil
}

// JSONPayload compacts JSON while preserving object key order from the input.
// It is useful when reproducing the JS/Android cross-language envelope vectors.
func JSONPayload(raw []byte) ([]byte, error) {
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, raw); err != nil {
		return nil, err
	}
	return []byte(buffer.String()), nil
}

func blake2bSum(size int, value []byte) []byte {
	hash, err := blake2b.New(size, nil)
	if err != nil {
		panic(err)
	}
	_, _ = hash.Write(value)
	return hash.Sum(nil)
}

func base32Lower(value []byte) string {
	return base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").
		WithPadding(base32.NoPadding).
		EncodeToString(value)
}
