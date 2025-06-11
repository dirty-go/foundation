package crypto

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"regexp"
	"strings"
)

type Certificate struct {
	der string

	*x509.Certificate
}

var (
	reNewLine           = regexp.MustCompile(`(?m)\\n`)
	substitutionNewLine = "\n"
)

// ParseCertificate parse raw data into x509.Certificate format
func ParseCertificate(certificate string) (*Certificate, error) {
	certificate = reNewLine.ReplaceAllString(certificate, substitutionNewLine)
	certificate = stringBetween(certificate, "-----BEGIN CERTIFICATE-----", "-----END CERTIFICATE-----")
	certificate = strings.TrimSpace(certificate)
	certificate = "-----BEGIN CERTIFICATE-----\n" + wordWrap(certificate, 75) + "\n-----END CERTIFICATE-----"
	block, _ := pem.Decode([]byte(certificate))
	if block == nil {
		return nil, fmt.Errorf("failed to parse certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: " + err.Error())
	}
	return &Certificate{
		der:         certificate,
		Certificate: cert,
	}, nil
}

func stringBetween(str string, start string, end string) string {
	startPos := strings.Index(str, start)
	if startPos == -1 {
		return ""
	}
	endPos := strings.Index(str, end)
	if endPos == -1 {
		return ""
	}
	firstPos := startPos + len(start)
	if firstPos >= endPos {
		return ""
	}
	return str[firstPos:endPos]
}

func wordWrap(text string, lineWidth int) (wrapped string) {
	words := strings.Fields(strings.TrimSpace(text))
	if len(words) == 0 {
		return text
	}
	wrapped = words[0]
	spaceLeft := lineWidth - len(wrapped)
	for _, word := range words[1:] {
		if len(word)+1 > spaceLeft {
			wrapped += "\n" + word
			spaceLeft = lineWidth - len(word)
		} else {
			wrapped += " " + word
			spaceLeft -= 1 + len(word)
		}
	}

	return
}

func (c *Certificate) PEM() []byte {
	return []byte(c.der)
}

func (c *Certificate) Algorithm() x509.PublicKeyAlgorithm {
	return c.PublicKeyAlgorithm
}
