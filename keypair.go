// go-multikeypair/keypair.go
//
// Based on IPFS go-multihash, with the aim of being a potential
// addition to the suite of multiformat project types.

// TODO CLI tool for working with multikeypairs.
// TODO: Investigate cosign/minisign for generating, encoding/decoding?

package multikeypair

import (
	"errors"
	//"fmt"

	"encoding/binary"
	b58 "github.com/mr-tron/base58/base58"
	varint "github.com/multiformats/go-varint"
	cryptobyte "golang.org/x/crypto/cryptobyte"
)

// Errors
// -----------------------------------------------------------------------------

const (
	MIN_KEY_LENGTH = 2
	MAX_KEY_LENGTH = 256
)

var (
	ErrUnknownCode         = errors.New("unknown multikeypair code")
	ErrTooShort            = errors.New("multikeypair too short. must be >= 2 bytes")
	ErrTooLong             = errors.New("multikeypair too long. must be < 129 bytes")
	ErrInvalidMultikeypair = errors.New("input isn't valid multikeypair")
)

// Ciphers
// -----------------------------------------------------------------------------

const (
	IDENTITY = uint64(0x00)
	ED_25519 = uint64(0x11)
)

// A mapping from cipher name to code.
var Names = map[string]uint64{
	"identity": IDENTITY,
	"ed25519":  ED_25519,
}

// A mapping from cipher code to name.
var Codes = map[uint64]string{
	IDENTITY: "identity",
	ED_25519: "ed25519",
}

// Keypair
// -----------------------------------------------------------------------------
// A keypair is an public/private keypair unpacked into a struct for
// easy access.

type Keypair struct {
	// Cipher identification code.
	Code uint64
	// Human-readable cipher name.
	Name string
	// Raw public key bytes.
	Public []byte
	// Length in bytes of public key.
	PublicLength int
	// Raw private key bytes.
	Private []byte
	// Length in bytes of private key.
	PrivateLength int
}

// Multikey
// -----------------------------------------------------------------------------
// A Multikey is a byte slice with the following form:
// [length] (24-bit length prefix)
//   [code length]<code> (16-bit length prefix, uvarint code)
//   [private key length]<private key> (16-bit length prefix)
//   [public key length]<public key> (16-bit length prefix)

type Multikeypair []byte

// Implementation
// -----------------------------------------------------------------------------

//
// ENCODE
//

// Encode a keypair into a Multikeypair, specifying the keypair type
// using an integer code.
func Encode(private []byte, public []byte, code uint64) (Multikeypair, error) {
	if err := validCode(code); err != nil {
		return Multikeypair{}, err
	}
	b := encodeKeypair(private, public, code)
	return Multikeypair(b), nil
}

// Encode a keypair into a Multikeypair, specifying the keypair type
// using a string name instead of an integer code.
func EncodeName(private []byte, public []byte, name string) (Multikeypair, error) {
	code := Names[name]
	return Encode(private, public, code)
}

// Encode a Keypair struct into a Multikeypair.
func (k Keypair) Encode() (Multikeypair, error) {
	if err := validCode(k.Code); err != nil {
		return Multikeypair{}, err
	}
	return Encode(k.Private, k.Public, k.Code)
}

// Check that the supplied code is one we recognize.
func validCode(code uint64) error {
	for k, _ := range Codes {
		if k == code {
			return nil
		}
	}
	return ErrUnknownCode
}

// Pack key material and code type into an array of bytes.
func encodeKeypair(private []byte, public []byte, code uint64) []byte {
	codeBuf := PackCode(code)

	var b cryptobyte.Builder

	b.AddUint24LengthPrefixed(func(b *cryptobyte.Builder) {
		// Store the code (packed as varint) with a length prefix.
		b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddBytes(codeBuf)
		})
		// Store the private key with a length prefix.
		b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddBytes(private)
		})
		// Store the public key with a length prefix.
		b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddBytes(public)
		})
	})

	result, err := b.Bytes()
	if err != nil {
		panic(err)
	}

	return result
}

//
// DECODE
//

// Unpack a multikeypair into a Keypair struct.
func Decode(m Multikeypair) (Keypair, error) {
	keypair, err := decodeKeypair([]byte(m))
	if err != nil {
		return Keypair{}, err
	}

	return *keypair, nil
}

// Unpack a multikeypair into a Keypair struct.
func (m Multikeypair) Decode() (Keypair, error) {
	return Decode(m)
}

func decodeKeypair(buf []byte) (*Keypair, error) {
	input := cryptobyte.String(buf)

	// Extract the overall length of the data.
	var values cryptobyte.String
	if !input.ReadUint24LengthPrefixed(&values) || !input.Empty() {
		return nil, ErrInvalidMultikeypair
	}

	// Extract the code (packed as a varint)
	var code cryptobyte.String
	if !values.ReadUint16LengthPrefixed(&code) {
		return nil, ErrInvalidMultikeypair
	}
	// Code is a varint that needs to be unpacked into a uint64.
	numCode, err := UnpackCode(code)
	if err != nil {
		return nil, err
	}

	var private cryptobyte.String
	if !values.ReadUint16LengthPrefixed(&private) {
		return nil, ErrInvalidMultikeypair
	}

	var public cryptobyte.String
	if !values.ReadUint16LengthPrefixed(&public) {
		return nil, ErrInvalidMultikeypair
	}

	// Check that the cipher type code we decoded is valid.
	if err := validCode(numCode); err != nil {
		return nil, err
	}
	name := Codes[numCode]
	privateLength := len(private)
	publicLength := len(public)

	keypair := &Keypair{
		Code:          numCode,
		Name:          name,
		Private:       private,
		PrivateLength: privateLength,
		Public:        public,
		PublicLength:  publicLength,
	}

	return keypair, nil
}

func castKeypair(buf []byte) (Multikeypair, error) {
	_, err := decodeKeypair(buf)
	if err != nil {
		return Multikeypair{}, err
	}

	return Multikeypair(buf), nil
}

//
// Base-58
//

// Generate a base58-encoded version of a Multikeypair.
func (m Multikeypair) B58String() string {
	return b58.Encode([]byte(m))
}

// Parse a base58-encoded hex string into a Multikeypair.
func MultikeypairFromB58(s string) (Multikeypair, error) {
	b, err := b58.Decode(s)
	if err != nil {
		return Multikeypair{}, ErrInvalidMultikeypair
	}

	// Test if is valid by attempting to decode as Keypair.
	_, err = decodeKeypair(b)
	if err != nil {
		return Multikeypair{}, err
	}

	return castKeypair(b)
}

func KeypairFromB58(s string) (Keypair, error) {
	mk, err := MultikeypairFromB58(s)
	if err != nil {
		return Keypair{}, err
	}

	// Now we have a nominal Multikeypair we can decode into a
	// Keypair struct.
	kp, err := mk.Decode()
	if err != nil {
		return Keypair{}, err
	}

	return kp, nil
}

//import (
//	"errors"
//
//)
//
// Errors
// -----------------------------------------------------------------------------

var (
	ErrVarintBufferShort = errors.New("uvarint: buffer too small")
	ErrVarintTooLong     = errors.New("uvarint: varint too big (max 64bit)")
)

// Utilities
// -----------------------------------------------------------------------------

// Pack cipher code as varint.
func PackCode(code uint64) []byte {
	// Encode a uint64 into a buffer and return number of bytes
	// written. Panics if the buffer is too small.
	size := varint.UvarintSize(code)
	buf := make([]byte, size)
	varint.PutUvarint(buf, code)
	return buf
}

func UnpackCode(buf []byte) (uint64, error) {
	// Returns number of bytes read if successful. On error the
	// value is 0 and the of bytes is <= 0, meaning:
	// n == 0: buffer too small
	// n < 0: value larger than 64 bits (overflow)
	code, n := binary.Uvarint(buf)

	if n == 0 {
		return 0, ErrVarintBufferShort
	} else if n < 0 {
		return 0, ErrVarintTooLong
	} else {
		return code, nil
	}
}