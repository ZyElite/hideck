package crypto

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"testing"
)

type minimalPRF struct{}

func (minimalPRF) Compute(key, data []byte) []byte { return []byte{byte(len(key) + len(data))} }
func (minimalPRF) KeyLen() int                     { return 1 }

var _ PRF = minimalPRF{}

type fallbackEncrypter struct{}

func (fallbackEncrypter) Encrypt(plaintext, key, iv, aad []byte) ([]byte, error) {
	return xorWithKey(plaintext, key), nil
}
func (fallbackEncrypter) Decrypt(ciphertext, key, iv, aad []byte) ([]byte, error) {
	return xorWithKey(ciphertext, key), nil
}
func (fallbackEncrypter) IVSize() int    { return 3 }
func (fallbackEncrypter) BlockSize() int { return 1 }
func (fallbackEncrypter) KeySize() int   { return 1 }

func xorWithKey(input, key []byte) []byte {
	result := append([]byte(nil), input...)
	for i := range result {
		result[i] ^= key[0]
	}
	return result
}

func TestRecoveredMODPGroupSizes(t *testing.T) {
	wantBits := map[uint16]int{
		1: 708, 2: 1024, 5: 1536, 14: 2048,
		15: 3432, 16: 4432, 17: 6432, 18: 8512,
	}
	for group, bits := range wantBits {
		dh, err := NewDiffieHellman(group)
		if err != nil {
			t.Fatalf("NewDiffieHellman(%d): %v", group, err)
		}
		if dh.Group != group || dh.P.BitLen() != bits || dh.G.Cmp(big.NewInt(2)) != 0 {
			t.Errorf("group %d = id:%d bits:%d generator:%s", group, dh.Group, dh.P.BitLen(), dh.G)
		}
	}
}

func TestRecoveredECDHGroups(t *testing.T) {
	for _, group := range []uint16{19, 20} {
		a, err := NewDiffieHellman(group)
		if err != nil {
			t.Fatalf("group %d: %v", group, err)
		}
		b, _ := NewDiffieHellman(group)
		if err := a.GenerateKey(); err != nil {
			t.Fatalf("group %d A: %v", group, err)
		}
		if err := b.GenerateKey(); err != nil {
			t.Fatalf("group %d B: %v", group, err)
		}
		secretA, err := a.ComputeSharedSecret(b.PublicKeyBytes())
		if err != nil {
			t.Fatalf("group %d secret A: %v", group, err)
		}
		secretB, err := b.ComputeSharedSecret(a.PublicKeyBytes())
		if err != nil || !bytes.Equal(secretA, secretB) {
			t.Fatalf("group %d secret mismatch: %v", group, err)
		}
	}
}

func TestDiffieHellmanRejectsBoundaryPublicKeys(t *testing.T) {
	dh, _ := NewDiffieHellman(14)
	dh.PrivateKey = bigOne()
	for _, peer := range [][]byte{{1}, newMinusOne(dh.P).Bytes()} {
		if _, err := dh.ComputeSharedSecret(peer); err == nil || err.Error() != "无效的对端公钥" {
			t.Fatalf("boundary peer error = %v", err)
		}
	}
}

func bigOne() *big.Int { return big.NewInt(1) }

func newMinusOne(value *big.Int) *big.Int {
	return new(big.Int).Sub(value, bigOne())
}

func TestEncryptionFactoriesMatchRecoveredContract(t *testing.T) {
	if EncrAESGCM8 != 18 || EncrAESGCM12 != 19 || EncrAESGCM16 != 20 {
		t.Fatalf("unexpected AES-GCM transform IDs: %d/%d/%d", EncrAESGCM8, EncrAESGCM12, EncrAESGCM16)
	}
	if _, err := GetEncrypterWithKeyLen(EncrAESGCM16, 0); err == nil || err.Error() != "AES-GCM 密钥长度未指定（keyLenBits=0），无法安全初始化加密器" {
		t.Fatalf("missing GCM key length error = %v", err)
	}
	if _, err := GetEncrypterWithKeyLen(EncrAESCBC, 130); err == nil || err.Error() != "无效的密钥长度" {
		t.Fatalf("invalid key length error = %v", err)
	}
	nullCipher, err := GetEncrypter(EncrNull)
	if err != nil || nullCipher.IVSize() != 0 || nullCipher.BlockSize() != 4 {
		t.Fatalf("null cipher = %#v, %v", nullCipher, err)
	}
}

func TestPrepareCipherSupportsOriginalAndAdditiveSelectors(t *testing.T) {
	key := mustHex(t, "2b7e151628aed2a6abf7158809cf4f3c")
	enc, err := GetEncrypterWithKeyLen(EncrAESCBC, 128)
	if err != nil {
		t.Fatal(err)
	}
	original, err := PrepareCipher(enc, key)
	if err != nil {
		t.Fatalf("original selector: %v", err)
	}
	additive, err := PrepareCipher(12, key)
	if err != nil {
		t.Fatalf("transform selector: %v", err)
	}
	if original.IVSize() != additive.IVSize() || original.BlockSize() != additive.BlockSize() {
		t.Fatal("selector forms produced different ciphers")
	}
}

func TestPrepareCipherFallbackPreservesAppendSemantics(t *testing.T) {
	prepared, err := PrepareCipher(fallbackEncrypter{}, []byte{0xff})
	if err != nil {
		t.Fatal(err)
	}
	prefix := []byte{0x01, 0x02}
	encrypted, err := prepared.Seal(prefix, []byte{0x10, 0x20}, nil, nil)
	if err != nil || !bytes.Equal(encrypted, []byte{0x01, 0x02, 0xef, 0xdf}) {
		t.Fatalf("fallback Seal = %x, %v", encrypted, err)
	}
	decrypted, err := prepared.Open(nil, encrypted[2:], nil, nil)
	if err != nil || !bytes.Equal(decrypted, []byte{0x10, 0x20}) {
		t.Fatalf("fallback Open = %x, %v", decrypted, err)
	}
	if prepared.IVSize() != 3 || prepared.BlockSize() != 1 {
		t.Fatalf("fallback sizes = %d/%d", prepared.IVSize(), prepared.BlockSize())
	}
}

func TestPreparedCBCVectorAndErrorPropagation(t *testing.T) {
	key := mustHex(t, "2b7e151628aed2a6abf7158809cf4f3c")
	iv := mustHex(t, "000102030405060708090a0b0c0d0e0f")
	plain := mustHex(t, "6bc1bee22e409f96e93d7e117393172a")
	want := mustHex(t, "7649abac8119b246cee98e9b12e9197d")
	prepared, err := PrepareCipher(EncrAESCBC, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := prepared.Seal(nil, plain, iv, nil)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("AES-CBC = %x, %v; want %x", got, err, want)
	}
	prefix := []byte("prefix")
	result, err := prepared.Seal(prefix, []byte{1}, iv, nil)
	if !errors.Is(err, errPlaintextAlignment) || !bytes.Equal(result, prefix) {
		t.Fatalf("unaligned Seal = %x, %v", result, err)
	}
}

func TestRecoveredGCMNonceCopyBehavior(t *testing.T) {
	key := bytes.Repeat([]byte{0x33}, 20)
	prepared, err := PrepareCipher(EncrAESGCM16, key)
	if err != nil {
		t.Fatal(err)
	}
	plain, aad := []byte("payload"), []byte("aad")
	iv := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	withLongIV, err := prepared.Seal(nil, plain, append(iv, 9, 10), aad)
	if err != nil {
		t.Fatal(err)
	}
	withExactIV, _ := prepared.Seal(nil, plain, iv, aad)
	if !bytes.Equal(withLongIV, withExactIV) {
		t.Fatal("GCM nonce did not truncate IV to the recovered eight-byte field")
	}
	withShortIV, _ := prepared.Seal(nil, plain, iv[:4], aad)
	withPaddedIV, _ := prepared.Seal(nil, plain, append(iv[:4:4], 0, 0, 0, 0), aad)
	if !bytes.Equal(withShortIV, withPaddedIV) {
		t.Fatal("GCM nonce did not zero-pad a short IV")
	}
}

func TestPRFRegistryAndOverflow(t *testing.T) {
	prf, err := GetPRF(1)
	if err != nil || prf != PRF_HMAC_MD5 {
		t.Fatalf("PRF HMAC-MD5 = %#v, %v", prf, err)
	}
	if output, err := PrfPlus(minimalPRF{}, nil, nil, 254); err != nil || len(output) != 254 {
		t.Fatalf("254-block PRF+ = %d bytes, %v", len(output), err)
	}
	if _, err := PrfPlus(minimalPRF{}, nil, nil, 255); err == nil || err.Error() != "PRF+ 溢出: 块太多" {
		t.Fatalf("PRF+ overflow error = %v", err)
	}
}

func TestAESXCBCPRFRFC4434(t *testing.T) {
	tests := []struct{ key, want string }{
		{"00010203040506070809", "0fa087af7d866e7653434e602fdde835"},
		{"000102030405060708090a0b0c0d0e0fedcb", "8cd3c93ae598a9803006ffb67c40e9e4"},
	}
	message := mustHex(t, "000102030405060708090a0b0c0d0e0f10111213")
	for _, test := range tests {
		got := aesXCBCPRF128(mustHex(t, test.key), message)
		if want := mustHex(t, test.want); !bytes.Equal(got, want) {
			t.Errorf("key %s: %x, want %x", test.key, got, want)
		}
	}
}

func TestWipeClearsEntireBuffer(t *testing.T) {
	secret := bytes.Repeat([]byte{0xa5}, 64)
	Wipe(secret)
	if !bytes.Equal(secret, make([]byte, len(secret))) {
		t.Fatalf("Wipe left data: %x", secret)
	}
}

func TestUnsupportedDiffieHellmanError(t *testing.T) {
	_, err := NewDiffieHellman(999)
	if got := fmt.Sprint(err); got != "不支持的 DH 组: 999" {
		t.Fatalf("error = %q", got)
	}
}
