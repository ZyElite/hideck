package crypto

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// DHGroup is an IKEv2 MODP Diffie-Hellman group (RFC 3526).
type DHGroup struct {
	GroupID   uint16
	Prime     *big.Int
	Generator *big.Int
}

// modp1024 is RFC 3526 group 2 (MODP-1024).
var modp1024 = &DHGroup{
	GroupID:   2,
	Prime:     mustBig("FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD129024E088A67CC74020BBEA63B139B22514A08798E3404DDEF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7EDEE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3DC2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F83655D23DCA3AD961C62F356208552BB9ED529077096966D670C354E4ABC9804F1746C08CA18217C32905E462E36CE3BE39E772C180E86039B2783A2EC07A28FB5C55DF06F4C52C9DE2BCBF6955817183995497CEA956AE515D2261898FA051015728E5A8AACAA68FFFFFFFFFFFFFFFF", 16),
	Generator: big.NewInt(2),
}

// modp2048 is RFC 3526 group 14 (MODP-2048).
var modp2048 = &DHGroup{
	GroupID:   14,
	Prime:     mustBig("FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD129024E088A67CC74020BBEA63B139B22514A08798E3404DDEF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7EDEE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3DC2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F83655D23DCA3AD961C62F356208552BB9ED529077096966D670C354E4ABC9804F1746C08CA18217C32905E462E36CE3BE39E772C180E86039B2783A2EC07A28FB5C55DF06F4C52C9DE2BCBF6955817183995497CEA956AE515D2261898FA051015728E5A8AACAA68FFFFFFFFFFFFFFFF", 16),
	Generator: big.NewInt(2),
}

func mustBig(s string, base int) *big.Int {
	n, ok := new(big.Int).SetString(s, base)
	if !ok {
		panic("crypto: bad group prime constant")
	}
	return n
}

// NewDiffieHellman returns a DH session for the given IKEv2 group ID.
func NewDiffieHellman(groupID uint16) (*DiffieHellman, error) {
	var g *DHGroup
	switch groupID {
	case 2:
		g = modp1024
	case 14:
		g = modp2048
	default:
		return nil, fmt.Errorf("crypto: unsupported DH group %d", groupID)
	}
	return &DiffieHellman{group: g}, nil
}

// DiffieHellman performs a single MODP Diffie-Hellman exchange.
type DiffieHellman struct {
	group      *DHGroup
	privateKey *big.Int
	publicKey  *big.Int
}

// GenerateKey derives a fresh private/public key pair.
func (d *DiffieHellman) GenerateKey() error {
	// exponent in [2, p-2]
	max := new(big.Int).Sub(d.group.Prime, big.NewInt(3))
	priv, err := rand.Int(rand.Reader, max)
	if err != nil {
		return err
	}
	priv.Add(priv, big.NewInt(2))
	d.privateKey = priv
	d.publicKey = new(big.Int).Exp(d.group.Generator, priv, d.group.Prime)
	return nil
}

// PublicKeyBytes returns the public key as a big-endian byte string (zero
// padded to the group prime size).
func (d *DiffieHellman) PublicKeyBytes() []byte {
	return padBytes(d.publicKey.Bytes(), d.group.Prime.BitLen()/8)
}

// ComputeSharedSecret derives the shared secret from the peer's public key.
func (d *DiffieHellman) ComputeSharedSecret(peer []byte) ([]byte, error) {
	if d.privateKey == nil {
		return nil, fmt.Errorf("crypto: no private key generated")
	}
	peerInt := new(big.Int).SetBytes(peer)
	if peerInt.Sign() <= 0 || peerInt.Cmp(d.group.Prime) >= 0 {
		return nil, fmt.Errorf("crypto: invalid peer public key")
	}
	secret := new(big.Int).Exp(peerInt, d.privateKey, d.group.Prime)
	return padBytes(secret.Bytes(), d.group.Prime.BitLen()/8), nil
}

// padBytes left-pads b to n bytes.
func padBytes(b []byte, n int) []byte {
	if len(b) >= n {
		return b
	}
	out := make([]byte, n)
	copy(out[n-len(b):], b)
	return out
}
