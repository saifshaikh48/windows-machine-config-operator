package crypto

import (
	"bytes"
	"io"
	"io/ioutil"

	"github.com/pkg/errors"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
)

// Encrypt creates an ecnrypted block of text from a plaintext message using the given key
func Encrypt(plainText string, key []byte) (string, error) {
	if key == nil {
		return "", errors.New("encryption passphrase cannot be nil")
	}

	// Prepare PGP block with a wrapper tag around the encrypted data
	tag := "ENCRYPTED DATA"
	msgBuffer := bytes.NewBuffer(nil)
	encoder, _ := armor.Encode(msgBuffer, tag, nil)

	writer, err := openpgp.SymmetricallyEncrypt(encoder, key, nil, nil)
	if err != nil {
		return "", errors.Wrap(err, "failure during encryption")
	}
	_, err = writer.Write([]byte(plainText))
	if err != nil {
		// Prevent leak in case of stream failure
		encoder.Close()
		return "", err
	}
	// Both writers must be closed before reading the bytes written to the buffer
	writer.Close()
	encoder.Close()

	return string(msgBuffer.Bytes()), nil
}

// Decrypt converts encrypted contents to plaintext using the given key as a passphrase
func Decrypt(encrypted string, key []byte) (string, error) {
	if key == nil {
		return "", errors.New("decryption passphrase cannot be empty")
	}

	// Unwrap encoded block holding the message content
	msgBuffer := bytes.NewBuffer([]byte(encrypted))
	armorBlock, err := armor.Decode(msgBuffer)
	if err != nil {
		return "", errors.Wrapf(err, "unable to process given data %s", encrypted)
	}

	msgBody, err := readMessage(armorBlock.Body, key)
	if err != nil {
		return "", err
	}
	plainTextBytes, err := ioutil.ReadAll(msgBody)
	if err != nil {
		return "", errors.Wrap(err, "unable to parse decrypted data into a readable value")
	}
	return string(plainTextBytes), nil
}

// readMessage attempts to read symmetrically encrypted data from the given reader
func readMessage(reader io.Reader, key []byte) (io.Reader, error) {
	// Flag needed to signal if the key has already been used and failed, else "function will be called again, forever"
	// documentation: https://godoc.org/golang.org/x/crypto/openpgp#PromptFunction
	attempted := false
	promptFunction := func(keys []openpgp.Key, symmetric bool) ([]byte, error) {
		if attempted {
			return nil, errors.New("invalid passphrase supplied")
		}
		attempted = true
		return key, nil
	}
	message, err := openpgp.ReadMessage(reader, nil, promptFunction, nil)
	if err != nil {
		return nil, errors.Wrap(err, "unable to decrypt message using given key")
	}
	return message.UnverifiedBody, nil
}
