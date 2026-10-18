// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package pgwire

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// scram logs in with SCRAM-SHA-256 (RFC 5802 and RFC 7677), the default
// password method since PostgreSQL 14. Channel binding isn't used.
func (c *Conn) scram(mechList []byte) error {
	offered := false
	for _, m := range strings.Split(string(mechList), "\x00") {
		if m == "SCRAM-SHA-256" {
			offered = true
		}
	}
	if !offered {
		return fmt.Errorf("the server offers only %q; LQO supports SCRAM-SHA-256 without channel binding", strings.Trim(string(mechList), "\x00"))
	}
	nonceRaw := make([]byte, 18)
	if _, err := rand.Read(nonceRaw); err != nil {
		return err
	}
	clientNonce := base64.RawStdEncoding.EncodeToString(nonceRaw)
	clientFirstBare := "n=,r=" + clientNonce
	clientFirst := "n,," + clientFirstBare

	b := []byte{'p', 0, 0, 0, 0}
	b = append(b, "SCRAM-SHA-256"...)
	b = append(b, 0)
	b = binary.BigEndian.AppendUint32(b, uint32(len(clientFirst)))
	b = append(b, clientFirst...)
	binary.BigEndian.PutUint32(b[1:], uint32(len(b)-1))
	if _, err := c.netConn.Write(b); err != nil {
		return err
	}

	serverFirst, err := c.saslMessage(11)
	if err != nil {
		return err
	}
	attrs := scramAttrs(serverFirst)
	nonce, saltB64, iterStr := attrs["r"], attrs["s"], attrs["i"]
	if !strings.HasPrefix(nonce, clientNonce) || len(nonce) == len(clientNonce) {
		return errors.New("SCRAM: the server's nonce doesn't extend ours")
	}
	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		return errors.New("SCRAM: bad salt from the server")
	}
	iter, err := strconv.Atoi(iterStr)
	if err != nil || iter < 1 {
		return errors.New("SCRAM: bad iteration count from the server")
	}

	salted, err := pbkdf2.Key(sha256.New, c.cfg.Password, salt, iter, 32)
	if err != nil {
		return err
	}
	clientKey := hmacSHA256(salted, "Client Key")
	storedKey := sha256.Sum256(clientKey)
	finalNoProof := "c=" + base64.StdEncoding.EncodeToString([]byte("n,,")) + ",r=" + nonce
	authMessage := clientFirstBare + "," + serverFirst + "," + finalNoProof
	signature := hmacSHA256(storedKey[:], authMessage)
	proof := make([]byte, len(clientKey))
	for i := range clientKey {
		proof[i] = clientKey[i] ^ signature[i]
	}
	final := finalNoProof + ",p=" + base64.StdEncoding.EncodeToString(proof)
	b = []byte{'p', 0, 0, 0, 0}
	b = append(b, final...)
	binary.BigEndian.PutUint32(b[1:], uint32(len(b)-1))
	if _, err := c.netConn.Write(b); err != nil {
		return err
	}

	serverFinal, err := c.saslMessage(12)
	if err != nil {
		return err
	}
	fa := scramAttrs(serverFinal)
	if e, ok := fa["e"]; ok {
		return fmt.Errorf("SCRAM: the server said %s", e)
	}
	got, err := base64.StdEncoding.DecodeString(fa["v"])
	if err != nil {
		return errors.New("SCRAM: bad server signature")
	}
	serverKey := hmacSHA256(salted, "Server Key")
	want := hmacSHA256(serverKey, authMessage)
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return errors.New("SCRAM: the server's signature doesn't match; it may not know the password")
	}
	return nil
}

// saslMessage reads the next authentication message and checks its code.
func (c *Conn) saslMessage(want uint32) (string, error) {
	for {
		typ, msg, err := c.readMsg()
		if err != nil {
			return "", err
		}
		switch typ {
		case 'E':
			return "", parseError(msg)
		case 'N':
			continue
		case 'R':
			if len(msg) < 4 {
				return "", errors.New("short authentication message")
			}
			if code := binary.BigEndian.Uint32(msg); code != want {
				return "", fmt.Errorf("SCRAM: expected step %d, got %d", want, code)
			}
			return string(msg[4:]), nil
		default:
			return "", fmt.Errorf("unexpected message %q during SCRAM", typ)
		}
	}
}

func scramAttrs(s string) map[string]string {
	m := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		if len(part) >= 2 && part[1] == '=' {
			m[part[:1]] = part[2:]
		}
	}
	return m
}

func hmacSHA256(key []byte, msg string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(msg))
	return h.Sum(nil)
}
