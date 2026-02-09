package test

import (
	"crypto/x509"
	"testing"

	"github.com/dmh2000/talkers/internal/tlsutil"
)

func TestGenerateSelfSignedCert(t *testing.T) {
	// Generate a self-signed certificate
	tlsCert, err := tlsutil.GenerateSelfSignedCert()
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert() failed: %v", err)
	}

	// Verify that the certificate was generated
	if len(tlsCert.Certificate) == 0 {
		t.Fatal("Expected at least one certificate in the chain")
	}

	// Parse the certificate
	cert, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}

	// Verify the Common Name
	if cert.Subject.CommonName != "sqirvy.xyz" {
		t.Errorf("Expected CN 'sqirvy.xyz', got '%s'", cert.Subject.CommonName)
	}

	// Verify the SAN (Subject Alternative Name)
	if len(cert.DNSNames) == 0 {
		t.Fatal("Expected at least one DNS name in SAN")
	}

	found := false
	for _, dnsName := range cert.DNSNames {
		if dnsName == "sqirvy.xyz" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected SAN to contain 'sqirvy.xyz', got %v", cert.DNSNames)
	}

	// Verify that the private key is present
	if tlsCert.PrivateKey == nil {
		t.Error("Expected private key to be present")
	}

	// Verify that the certificate is not yet expired
	if cert.NotAfter.Before(cert.NotBefore) {
		t.Error("Certificate NotAfter is before NotBefore")
	}

	// Verify key usage
	expectedKeyUsage := x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature
	if cert.KeyUsage&expectedKeyUsage != expectedKeyUsage {
		t.Errorf("Expected key usage to include KeyEncipherment and DigitalSignature, got %v", cert.KeyUsage)
	}

	// Verify extended key usage
	if len(cert.ExtKeyUsage) == 0 {
		t.Fatal("Expected at least one extended key usage")
	}
	foundServerAuth := false
	for _, eku := range cert.ExtKeyUsage {
		if eku == x509.ExtKeyUsageServerAuth {
			foundServerAuth = true
			break
		}
	}
	if !foundServerAuth {
		t.Error("Expected extended key usage to include ServerAuth")
	}
}
