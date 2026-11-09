package protocol

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type protocolVectors struct {
	Host struct {
		Seed              string `json:"seed"`
		PublicKeyHex      string `json:"publicKeyHex"`
		CurvePublicKeyHex string `json:"curvePublicKeyHex"`
		DeviceID          string `json:"deviceId"`
		Fingerprint       string `json:"fingerprint"`
	} `json:"host"`
	Client struct {
		Seed              string `json:"seed"`
		PublicKeyHex      string `json:"publicKeyHex"`
		CurvePublicKeyHex string `json:"curvePublicKeyHex"`
		DeviceID          string `json:"deviceId"`
		Fingerprint       string `json:"fingerprint"`
	} `json:"client"`
	Hello struct {
		Nonce        string `json:"nonce"`
		TS           string `json:"ts"`
		SignatureHex string `json:"signatureHex"`
	} `json:"hello"`
	SAS struct {
		Words []string `json:"words"`
	} `json:"sas"`
	Envelope struct {
		Payload json.RawMessage `json:"payload"`
		Envelope
	} `json:"envelope"`
}

func loadVectors(t *testing.T) protocolVectors {
	t.Helper()
	raw, err := os.ReadFile("../../packages/protocol-js/test-vectors.json")
	if err != nil {
		t.Fatalf("read test vectors: %v", err)
	}
	var vectors protocolVectors
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatalf("decode test vectors: %v", err)
	}
	return vectors
}

func mustHex(t *testing.T, value string) []byte {
	t.Helper()
	out, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCrossLanguageIdentityAndSASVectors(t *testing.T) {
	vectors := loadVectors(t)
	host, err := KeyPairFromSeedString(vectors.Host.Seed)
	if err != nil {
		t.Fatal(err)
	}
	client, err := KeyPairFromSeedString(vectors.Client.Seed)
	if err != nil {
		t.Fatal(err)
	}

	if got := hex.EncodeToString(host.PublicKey); got != vectors.Host.PublicKeyHex {
		t.Fatalf("host public key = %s, want %s", got, vectors.Host.PublicKeyHex)
	}
	if got := hex.EncodeToString(client.PublicKey); got != vectors.Client.PublicKeyHex {
		t.Fatalf("client public key = %s, want %s", got, vectors.Client.PublicKeyHex)
	}
	hostCurve, err := CurveKeys(host)
	if err != nil {
		t.Fatal(err)
	}
	clientCurve, err := CurveKeys(client)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(hostCurve.PublicKey[:]); got != vectors.Host.CurvePublicKeyHex {
		t.Fatalf("host curve public key = %s, want %s", got, vectors.Host.CurvePublicKeyHex)
	}
	if got := hex.EncodeToString(clientCurve.PublicKey[:]); got != vectors.Client.CurvePublicKeyHex {
		t.Fatalf("client curve public key = %s, want %s", got, vectors.Client.CurvePublicKeyHex)
	}
	if got := DeviceID(host.PublicKey); got != vectors.Host.DeviceID {
		t.Fatalf("host device id = %s, want %s", got, vectors.Host.DeviceID)
	}
	if got := DeviceID(client.PublicKey); got != vectors.Client.DeviceID {
		t.Fatalf("client device id = %s, want %s", got, vectors.Client.DeviceID)
	}
	if got := Fingerprint(host.PublicKey); got != vectors.Host.Fingerprint {
		t.Fatalf("host fingerprint = %s, want %s", got, vectors.Host.Fingerprint)
	}
	if got := Fingerprint(client.PublicKey); got != vectors.Client.Fingerprint {
		t.Fatalf("client fingerprint = %s, want %s", got, vectors.Client.Fingerprint)
	}
	if !VerifyHostFingerprint(host.PublicKey, vectors.Host.Fingerprint) {
		t.Fatal("valid fingerprint rejected")
	}
	if VerifyHostFingerprint(host.PublicKey, "0000000000000000") {
		t.Fatal("wrong fingerprint accepted")
	}

	sas, err := SASWords(host.PublicKey, client.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors.SAS.Words) != 4 {
		t.Fatalf("vector SAS has %d words, want 4", len(vectors.SAS.Words))
	}
	for index := range sas {
		if sas[index] != vectors.SAS.Words[index] {
			t.Fatalf("SAS[%d] = %s, want %s", index, sas[index], vectors.SAS.Words[index])
		}
	}
}

func TestHelloSignatureVector(t *testing.T) {
	vectors := loadVectors(t)
	host, err := KeyPairFromSeedString(vectors.Host.Seed)
	if err != nil {
		t.Fatal(err)
	}
	want := mustHex(t, vectors.Hello.SignatureHex)
	got, err := HelloSignature(host.PrivateKey, vectors.Hello.Nonce, vectors.Hello.TS)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("hello signature = %x, want %x", got, want)
	}
	if !VerifyHelloSignature(host.PublicKey, vectors.Hello.Nonce, vectors.Hello.TS, want) {
		t.Fatal("hello signature did not verify")
	}
	if VerifyHelloSignature(host.PublicKey, vectors.Hello.Nonce, vectors.Hello.TS+"x", want) {
		t.Fatal("tampered hello timestamp verified")
	}
}

func TestEnvelopeVectorDecryptsAndReencryptsByteForByte(t *testing.T) {
	vectors := loadVectors(t)
	host, err := KeyPairFromSeedString(vectors.Host.Seed)
	if err != nil {
		t.Fatal(err)
	}
	client, err := KeyPairFromSeedString(vectors.Client.Seed)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := JSONPayload(vectors.Envelope.Payload)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := OpenEnvelope(vectors.Envelope.Envelope, client, host.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(opened, payload) {
		t.Fatalf("opened payload = %s, want %s", opened, payload)
	}
	nonce, err := FromBase64(vectors.Envelope.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := CreateEnvelopeWithNonce(
		vectors.Envelope.From,
		vectors.Envelope.To,
		payload,
		host,
		client.PublicKey,
		vectors.Envelope.ID,
		vectors.Envelope.TS,
		nonce,
	)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.CT != vectors.Envelope.CT {
		t.Fatalf("ciphertext differs:\n got %s\nwant %s", rebuilt.CT, vectors.Envelope.CT)
	}
	if rebuilt.Nonce != vectors.Envelope.Nonce {
		t.Fatalf("nonce differs: got %s want %s", rebuilt.Nonce, vectors.Envelope.Nonce)
	}
}

func TestTamperedCiphertextIsRejected(t *testing.T) {
	vectors := loadVectors(t)
	host, err := KeyPairFromSeedString(vectors.Host.Seed)
	if err != nil {
		t.Fatal(err)
	}
	client, err := KeyPairFromSeedString(vectors.Client.Seed)
	if err != nil {
		t.Fatal(err)
	}
	envelope := vectors.Envelope.Envelope
	envelope.CT = strings.TrimSuffix(envelope.CT, "AAAA") + "BBBB"
	if _, err := OpenEnvelope(envelope, client, host.PublicKey); err == nil {
		t.Fatal("tampered ciphertext decrypted successfully")
	}
}

func TestPairingURIAndTransportHelpers(t *testing.T) {
	invite := PairingInvite{
		Server:      "wss://relay.example.test/v1/ws",
		Token:       "demo-pair-token",
		HostID:      "demo-host",
		Fingerprint: "0011223344556677",
	}
	parsed, err := ParsePairURI(PairURI(invite))
	if err != nil {
		t.Fatal(err)
	}
	if parsed != invite {
		t.Fatalf("parsed invite = %+v, want %+v", parsed, invite)
	}
	normalized, err := NormalizeWebSocketURL("https://relay.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if normalized != "wss://relay.example.test/v1/ws" {
		t.Fatalf("normalized URL = %s", normalized)
	}
	code, err := RandomCode6()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 {
		t.Fatalf("code6 = %q", code)
	}
	token, err := RandomPairToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 32 {
		t.Fatalf("pair token = %q", token)
	}
	if len(NewULID()) != 26 {
		t.Fatal("ULID is not 26 characters")
	}
}
