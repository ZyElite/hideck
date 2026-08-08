package crypto

import (
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
)

var (
	prime768  = mustPrime(prime768Hex)
	prime1024 = mustPrime(prime1024Hex)
	prime1536 = mustPrime(prime1536Hex)
	prime2048 = mustPrime(prime2048Hex)
	prime3072 = mustPrime(prime3072Hex)
	prime4096 = mustPrime(prime4096Hex)
	prime6144 = mustPrime(prime6144Hex)
	prime8192 = mustPrime(prime8192Hex)
	gen2      = big.NewInt(2)
)

type DiffieHellman struct {
	Group      uint16
	PrivateKey *big.Int
	PublicKey  *big.Int
	SharedKey  []byte
	P          *big.Int
	G          *big.Int

	ecdhCurve   ecdh.Curve
	ecdhPrivKey *ecdh.PrivateKey
	ecdhPubKey  *ecdh.PublicKey
}

func NewDiffieHellman(group uint16) (*DiffieHellman, error) {
	dh := &DiffieHellman{Group: group}
	switch group {
	case 1:
		dh.P, dh.G = prime768, gen2
	case 2:
		dh.P, dh.G = prime1024, gen2
	case 5:
		dh.P, dh.G = prime1536, gen2
	case 14:
		dh.P, dh.G = prime2048, gen2
	case 15:
		dh.P, dh.G = prime3072, gen2
	case 16:
		dh.P, dh.G = prime4096, gen2
	case 17:
		dh.P, dh.G = prime6144, gen2
	case 18:
		dh.P, dh.G = prime8192, gen2
	case 19:
		dh.ecdhCurve = ecdh.P256()
	case 20:
		dh.ecdhCurve = ecdh.P384()
	default:
		return nil, fmt.Errorf("不支持的 DH 组: %d", group)
	}
	return dh, nil
}

func (dh *DiffieHellman) GenerateKey() error {
	if dh.ecdhCurve != nil {
		privateKey, err := dh.ecdhCurve.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		dh.ecdhPrivKey = privateKey
		dh.ecdhPubKey = privateKey.PublicKey()
		return nil
	}
	privateKey, err := rand.Int(rand.Reader, dh.P)
	if err != nil {
		return err
	}
	dh.PrivateKey = privateKey
	dh.PublicKey = new(big.Int).Exp(dh.G, privateKey, dh.P)
	return nil
}

func (dh *DiffieHellman) ComputeSharedSecret(peerPublicKey []byte) ([]byte, error) {
	if dh.ecdhCurve != nil {
		return dh.computeECDHSecret(peerPublicKey)
	}
	peer := new(big.Int).SetBytes(peerPublicKey)
	one := big.NewInt(1)
	pMinusOne := new(big.Int).Sub(dh.P, one)
	if peer.Cmp(one) <= 0 || peer.Cmp(pMinusOne) >= 0 {
		return nil, errors.New("无效的对端公钥")
	}
	secret := new(big.Int).Exp(peer, dh.PrivateKey, dh.P)
	dh.SharedKey = leftPad(secret.Bytes(), (dh.P.BitLen()+7)/8)
	return dh.SharedKey, nil
}

func (dh *DiffieHellman) computeECDHSecret(peerPublicKey []byte) ([]byte, error) {
	if dh.ecdhPrivKey == nil {
		return nil, errors.New("ECDH 私钥未初始化")
	}
	peer, err := dh.ecdhCurve.NewPublicKey(peerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("无效的对端公钥: %w", err)
	}
	secret, err := dh.ecdhPrivKey.ECDH(peer)
	if err != nil {
		return nil, err
	}
	dh.SharedKey = append(dh.SharedKey[:0], secret...)
	return dh.SharedKey, nil
}

func (dh *DiffieHellman) PublicKeyBytes() []byte {
	if dh.ecdhPubKey != nil {
		return append([]byte(nil), dh.ecdhPubKey.Bytes()...)
	}
	return leftPad(dh.PublicKey.Bytes(), (dh.P.BitLen()+7)/8)
}

func leftPad(value []byte, length int) []byte {
	if len(value) >= length {
		return value
	}
	padded := make([]byte, length)
	copy(padded[length-len(value):], value)
	return padded
}

func mustPrime(value string) *big.Int {
	prime, ok := new(big.Int).SetString(value, 16)
	if !ok {
		panic("invalid MODP prime")
	}
	return prime
}
