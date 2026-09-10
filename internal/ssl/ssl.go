package ssl

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/md5"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	nethttp "net/http"
	"os"
	"slices"
	"strings"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"golang.org/x/term"
)

// Determine if a string element is in a string array
func HasString(elem string, arr []string) bool {
	return slices.Contains(arr, elem)
}

// NewTLSConfig returns an initialized TLS configuration suitable for client
// authentication. If caFile is non-empty, it will be loaded.
func NewTLSConfig(caFile string, verifyCert bool) (*tls.Config, error) {
	var c tls.Config

	// Set to TLS 1.2 as a minimum.  This is overridden for mysql communication
	c.MinVersion = tls.VersionTLS12
	// "If CipherSuites is nil, a default list of secure cipher suites is used"
	c.CipherSuites = nil

	if verifyCert {
		log.Info("verifyCert requested, client certificates will be verified")
		c.ClientAuth = tls.VerifyClientCertIfGiven
	}
	caPool, err := ReadCAFile(caFile)
	if err != nil {
		return &c, err
	}
	c.ClientCAs = caPool
	c.BuildNameToCertificate()
	return &c, nil
}

// Returns CA certificate. If caFile is non-empty, it will be loaded.
func ReadCAFile(caFile string) (*x509.CertPool, error) {
	var caCertPool *x509.CertPool
	if caFile != "" {
		data, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		caCertPool = x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(data) {
			return nil, errors.New("no certificates parsed")
		}
		log.Info("Read in CA file:", caFile)
	}
	return caCertPool, nil
}

// Verify that the OU of the presented client certificate matches the list
// of Valid OUs
func Verify(r *nethttp.Request, validOUs []string) error {
	if strings.Contains(r.URL.String(), config.Config.Server.Status.Endpoint) && !config.Config.Server.Status.VerifyOU {
		return nil
	}
	if r.TLS == nil {
		return errors.New("No TLS")
	}
	for _, chain := range r.TLS.VerifiedChains {
		s := chain[0].Subject.OrganizationalUnit
		log.Debug("All OUs:", strings.Join(s, " "))
		for _, ou := range s {
			log.Debug("Client presented OU:", ou)
			if HasString(ou, validOUs) {
				log.Debug("Found valid OU:", ou)
				return nil
			}
		}
	}
	log.Error("No valid OUs found")
	return errors.New("Invalid OU")
}

// VerifyOUs returns a request verifier suitable for the HTTP transport.
func VerifyOUs(validOUs []string) func(*nethttp.Request) error {
	return func(req *nethttp.Request) error {
		log.Debug("Verifying client OU")
		return Verify(req, validOUs)
	}
}

// AppendKeyPair loads the given TLS key pair and appends it to
// tlsConfig.Certificates.
func AppendKeyPair(tlsConfig *tls.Config, certFile string, keyFile string) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}
	tlsConfig.Certificates = append(tlsConfig.Certificates, cert)
	return nil
}

// Read in a keypair where the key is password protected
func AppendKeyPairWithPassword(tlsConfig *tls.Config, certFile string, keyFile string, pemPass []byte) error {

	// Certificates aren't usually password protected, but we're kicking the password
	// along just in case.  It won't be used if the file isn't encrypted
	certData, err := ReadPEMData(certFile, pemPass)
	if err != nil {
		return err
	}
	keyData, err := ReadPEMData(keyFile, pemPass)
	if err != nil {
		return err
	}
	cert, err := tls.X509KeyPair(certData, keyData)
	if err != nil {
		return err
	}
	tlsConfig.Certificates = append(tlsConfig.Certificates, cert)
	return nil
}

// Read a PEM file and ask for a password to decrypt it if needed
func ReadPEMData(pemFile string, pemPass []byte) ([]byte, error) {
	pemData, err := os.ReadFile(pemFile)
	if err != nil {
		return pemData, err
	}

	// We should really just get the pem.Block back here, if there's other
	// junk on the end, warn about it.
	pemBlock, rest := pem.Decode(pemData)
	if len(rest) > 0 {
		log.Warning("Didn't parse all of", pemFile)
	}

	if pemBlock == nil {
		return nil, fmt.Errorf("no PEM data found in %s", pemFile)
	}
	if isLegacyEncryptedPEMBlock(pemBlock) {
		// Decrypt and get the ASN.1 DER bytes here
		pemData, err = decryptLegacyPEMBlock(pemBlock, pemPass)
		if err != nil {
			return pemData, err
		} else {
			log.Info("Decrypted", pemFile, "successfully")
		}
		// Shove the decrypted DER bytes into a new pem Block with blank headers
		var newBlock pem.Block
		newBlock.Type = pemBlock.Type
		newBlock.Bytes = pemData
		// This is now like reading in an unencrypted key from a file and stuffing it
		// into a byte stream
		pemData = pem.EncodeToMemory(&newBlock)
	}
	return pemData, nil
}

// Print a password prompt on the terminal and collect a password
func GetPEMPassword(pemFile string) []byte {
	fmt.Printf("Password for %s: ", pemFile)
	pass, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		// The legacy PEM decoder will report an incorrect password when detectable.
		return []byte("")
	}
	return pass
}

// Determine if PEM file is encrypted
func IsEncryptedPEM(pemFile string) bool {
	pemData, err := os.ReadFile(pemFile)
	if err != nil {
		return false
	}
	pemBlock, _ := pem.Decode(pemData)
	if pemBlock == nil || len(pemBlock.Bytes) == 0 {
		return false
	}
	return isLegacyEncryptedPEMBlock(pemBlock)
}

type legacyPEMCipher struct {
	name      string
	keySize   int
	blockSize int
	newCipher func([]byte) (cipher.Block, error)
}

// legacyPEMCiphers preserves the existing RFC 1423 encrypted-key contract.
// New configurations should prefer unencrypted key files protected by file
// permissions until an authenticated encrypted-key format is supported.
var legacyPEMCiphers = []legacyPEMCipher{
	{name: "DES-CBC", keySize: 8, blockSize: des.BlockSize, newCipher: des.NewCipher},
	{name: "DES-EDE3-CBC", keySize: 24, blockSize: des.BlockSize, newCipher: des.NewTripleDESCipher},
	{name: "AES-128-CBC", keySize: 16, blockSize: aes.BlockSize, newCipher: aes.NewCipher},
	{name: "AES-192-CBC", keySize: 24, blockSize: aes.BlockSize, newCipher: aes.NewCipher},
	{name: "AES-256-CBC", keySize: 32, blockSize: aes.BlockSize, newCipher: aes.NewCipher},
}

func isLegacyEncryptedPEMBlock(block *pem.Block) bool {
	if block == nil {
		return false
	}
	_, ok := block.Headers["DEK-Info"]
	return ok
}

func decryptLegacyPEMBlock(block *pem.Block, password []byte) ([]byte, error) {
	dekInfo, ok := block.Headers["DEK-Info"]
	if !ok {
		return nil, errors.New("x509: no DEK-Info header in block")
	}

	mode, encodedIV, ok := strings.Cut(dekInfo, ",")
	if !ok {
		return nil, errors.New("x509: malformed DEK-Info header")
	}

	var algorithm *legacyPEMCipher
	for i := range legacyPEMCiphers {
		if legacyPEMCiphers[i].name == mode {
			algorithm = &legacyPEMCiphers[i]
			break
		}
	}
	if algorithm == nil {
		return nil, errors.New("x509: unknown encryption mode")
	}

	iv, err := hex.DecodeString(encodedIV)
	if err != nil {
		return nil, err
	}
	if len(iv) != algorithm.blockSize {
		return nil, errors.New("x509: incorrect IV size")
	}

	key := deriveLegacyPEMKey(password, iv[:8], algorithm.keySize)
	blockCipher, err := algorithm.newCipher(key)
	if err != nil {
		return nil, err
	}
	if len(block.Bytes)%blockCipher.BlockSize() != 0 {
		return nil, errors.New("x509: encrypted PEM data is not a multiple of the block size")
	}

	data := make([]byte, len(block.Bytes))
	cipher.NewCBCDecrypter(blockCipher, iv).CryptBlocks(data, block.Bytes)
	if len(data) == 0 {
		return nil, errors.New("x509: invalid padding")
	}
	paddingLength := int(data[len(data)-1])
	if paddingLength == 0 || paddingLength > algorithm.blockSize || paddingLength > len(data) {
		return nil, x509.IncorrectPasswordError
	}
	for _, value := range data[len(data)-paddingLength:] {
		if int(value) != paddingLength {
			return nil, x509.IncorrectPasswordError
		}
	}
	return data[:len(data)-paddingLength], nil
}

func deriveLegacyPEMKey(password, salt []byte, keySize int) []byte {
	hash := md5.New()
	key := make([]byte, keySize)
	var digest []byte
	for i := 0; i < len(key); i += len(digest) {
		hash.Reset()
		hash.Write(digest)
		hash.Write(password)
		hash.Write(salt)
		digest = hash.Sum(digest[:0])
		copy(key[i:], digest)
	}
	return key
}

// ListenAndServeTLS acts identically to http.ListenAndServeTLS, except that it
// expects TLS configuration.
// TODO: refactor so this is testable?
func ListenAndServeTLS(addr string, handler nethttp.Handler, tlsConfig *tls.Config) error {
	if addr == "" {
		// On unix Listen calls getaddrinfo to parse the port, so named ports are fine as long
		// as they exist in /etc/services
		addr = ":https"
	}
	l, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		return err
	}
	return nethttp.Serve(l, handler)
}
