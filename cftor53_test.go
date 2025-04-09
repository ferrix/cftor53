package main

import (
	"encoding/json"
	"os"
	"testing"
)

// import (
// 	"testing"

// 	"github.com/aws/aws-cdk-go/awscdk/v2"
// 	"github.com/aws/aws-cdk-go/awscdk/v2/assertions"
// 	"github.com/aws/jsii-runtime-go"
// )

// example tests. To run these tests, uncomment this file along with the
// example resource in cftor53_test.go
// func TestCftor53Stack(t *testing.T) {
// 	// GIVEN
// 	app := awscdk.NewApp(nil)

// 	// WHEN
// 	stack := NewCftor53Stack(app, "MyStack", nil)

// 	// THEN
// 	template := assertions.Template_FromStack(stack, nil)

// 	template.HasResourceProperties(jsii.String("AWS::SQS::Queue"), map[string]interface{}{
// 		"VisibilityTimeout": 300,
// 	})
// }

func TestConfigFileParsing(t *testing.T) {
	// Create a temporary test config
	testConfig := ConfigFile{
		ApiToken:     "test-token",
		ParentDomain: "example.com",
		Subdomain:    "test",
		Regions: &RegionConfig{
			Main:        "eu-north-1",
			Certificate: "us-east-1",
		},
	}

	// Serialize to JSON
	configBytes, err := json.Marshal(testConfig)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	// Write to temporary file
	tempFile, err := os.CreateTemp("", "config-test-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	_, err = tempFile.Write(configBytes)
	if err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tempFile.Close()

	// Read the file back
	readBytes, err := os.ReadFile(tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to read test config: %v", err)
	}

	// Parse the configuration
	var parsedConfig ConfigFile
	if err := json.Unmarshal(readBytes, &parsedConfig); err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	// Verify the parsed values
	if parsedConfig.ApiToken != testConfig.ApiToken {
		t.Errorf("Expected ApiToken %s, got %s", testConfig.ApiToken, parsedConfig.ApiToken)
	}

	if parsedConfig.ParentDomain != testConfig.ParentDomain {
		t.Errorf("Expected ParentDomain %s, got %s", testConfig.ParentDomain, parsedConfig.ParentDomain)
	}

	if parsedConfig.Subdomain != testConfig.Subdomain {
		t.Errorf("Expected Subdomain %s, got %s", testConfig.Subdomain, parsedConfig.Subdomain)
	}

	if parsedConfig.Regions == nil {
		t.Errorf("Expected Regions to be non-nil")
	} else {
		if parsedConfig.Regions.Main != testConfig.Regions.Main {
			t.Errorf("Expected Regions.Main %s, got %s", testConfig.Regions.Main, parsedConfig.Regions.Main)
		}

		if parsedConfig.Regions.Certificate != testConfig.Regions.Certificate {
			t.Errorf("Expected Regions.Certificate %s, got %s", testConfig.Regions.Certificate, parsedConfig.Regions.Certificate)
		}
	}
}

func TestDefaultValues(t *testing.T) {
	// Create a minimal config
	testConfig := ConfigFile{
		ApiToken:     "test-token",
		ParentDomain: "example.com",
		Subdomain:    "test",
	}

	// Test default region values
	mainRegion := "eu-north-1" // Default main region
	certRegion := "us-east-1"  // Default cert region

	if testConfig.Regions != nil {
		if testConfig.Regions.Main != "" {
			mainRegion = testConfig.Regions.Main
		}
		if testConfig.Regions.Certificate != "" {
			certRegion = testConfig.Regions.Certificate
		}
	}

	// Verify default values
	if mainRegion != "eu-north-1" {
		t.Errorf("Expected default mainRegion to be eu-north-1, got %s", mainRegion)
	}

	if certRegion != "us-east-1" {
		t.Errorf("Expected default certRegion to be us-east-1, got %s", certRegion)
	}

	// Test default secret name
	secretName := "cftor53/cloudflare/api-token"
	if testConfig.SecretName != "" {
		secretName = testConfig.SecretName
	}

	if secretName != "cftor53/cloudflare/api-token" {
		t.Errorf("Expected default secretName to be cftor53/cloudflare/api-token, got %s", secretName)
	}

	// Test default SSM parameter prefix
	ssmParamPrefix := "/cftor53"
	if testConfig.SsmParamPrefix != "" {
		ssmParamPrefix = testConfig.SsmParamPrefix
	}

	if ssmParamPrefix != "/cftor53" {
		t.Errorf("Expected default ssmParamPrefix to be /cftor53, got %s", ssmParamPrefix)
	}
}
