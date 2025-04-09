package main

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscertificatemanager"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsiam"
	"github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsroute53"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssecretsmanager"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsssm"
	"github.com/aws/constructs-go/constructs/v10"
	"github.com/aws/jsii-runtime-go"
)

// ConfigFile represents the structure of the config.json file
type ConfigFile struct {
	ApiToken       string                `json:"api_token"`
	ParentDomain   string                `json:"parent_domain"`
	Subdomain      string                `json:"subdomain"`
	SecretName     string                `json:"secret_name,omitempty"`
	SsmParamPrefix string                `json:"ssm_param_prefix,omitempty"`
	LambdaSettings *LambdaSettingsConfig `json:"lambda_settings,omitempty"`
	Regions        *RegionConfig         `json:"regions,omitempty"`
}

// RegionConfig represents the region configuration
type RegionConfig struct {
	Main        string `json:"main"`
	Certificate string `json:"certificate"`
}

// LambdaSettingsConfig represents the configuration for Lambda functions
type LambdaSettingsConfig struct {
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	MemorySizeMB   int `json:"memory_size_mb,omitempty"`
}

type Cftor53StackProps struct {
	awscdk.StackProps

	// Domain hosted on Cloudflare
	ParentDomain *string

	// Subdomain to be hosted on Route53
	Subdomain *string

	// Cloudflare API token secret
	CloudflareApiTokenSecret awssecretsmanager.ISecret

	// Configuration settings
	Config *ConfigFile
}

// Main stack for Route53 hosted zone
func NewCftor53Stack(scope constructs.Construct, id string, props *Cftor53StackProps) (awscdk.Stack, *string) {
	var sprops awscdk.StackProps
	if props != nil {
		sprops = props.StackProps
	}
	stack := awscdk.NewStack(scope, &id, &sprops)

	// Validate required properties
	if props.ParentDomain == nil || props.Subdomain == nil || props.Config == nil {
		panic("ParentDomain, Subdomain and Config must be provided")
	}

	// Full domain name for the subdomain (e.g., sub.example.com)
	fullDomainName := jsii.String(*props.Subdomain + "." + *props.ParentDomain)

	// Create a secret for the Cloudflare API token if not provided from another stack
	var cloudflareSecret awssecretsmanager.ISecret
	if props.CloudflareApiTokenSecret != nil {
		cloudflareSecret = props.CloudflareApiTokenSecret
	} else if props.Config.ApiToken != "" {
		// Create a local secret using the API token from config
		cloudflareSecret = awssecretsmanager.NewSecret(stack, jsii.String("LocalCloudflareApiToken"), &awssecretsmanager.SecretProps{
			Description: jsii.String("Cloudflare API Token for DNS management"),
			SecretName:  jsii.String("cftor53/cloudflare/api-token-local"),
			SecretObjectValue: &map[string]awscdk.SecretValue{
				"api_token": awscdk.SecretValue_UnsafePlainText(jsii.String(props.Config.ApiToken)),
			},
		})
	} else {
		panic("Either CloudflareApiTokenSecret or Config.ApiToken must be provided")
	}

	// Create a custom resource to check for colliding DNS records in Cloudflare
	// but not make any changes yet
	checkRecordsLambda := awslambda.NewFunction(stack, jsii.String("CloudflareCheckDNSLambda"), &awslambda.FunctionProps{
		Runtime:      awslambda.Runtime_PROVIDED_AL2(),
		Handler:      jsii.String("bootstrap"),
		Code:         awslambda.Code_FromAsset(jsii.String("lambda/main.zip"), nil),
		Timeout:      awscdk.Duration_Seconds(jsii.Number(float64(props.Config.LambdaSettings.TimeoutSeconds))),
		MemorySize:   jsii.Number(float64(props.Config.LambdaSettings.MemorySizeMB)),
		Architecture: awslambda.Architecture_X86_64(),
	})

	// Grant permissions to read the Cloudflare API token secret
	// Create an explicit policy statement to grant read access to the secret
	secretArn := cloudflareSecret.SecretArn()
	checkRecordsLambda.AddToRolePolicy(awsiam.NewPolicyStatement(&awsiam.PolicyStatementProps{
		Actions:   jsii.Strings("secretsmanager:GetSecretValue", "secretsmanager:DescribeSecret"),
		Resources: jsii.Strings(*secretArn),
	}))

	// First custom resource: only checks for colliding DNS records
	checkDnsResource := awscdk.NewCustomResource(stack, jsii.String("CloudflareDNSCollisionChecker"), &awscdk.CustomResourceProps{
		ServiceToken: checkRecordsLambda.FunctionArn(),
		Properties: &map[string]interface{}{
			"Domain":    *props.ParentDomain,
			"Subdomain": *props.Subdomain,
			"SecretId":  cloudflareSecret.SecretName(),
			"Action":    "check", // Signal to Lambda to only check, not update
		},
	})

	// Create a Route53 hosted zone for the subdomain - depends on the check
	hostedZone := awsroute53.NewPublicHostedZone(stack, jsii.String("SubdomainHostedZone"), &awsroute53.PublicHostedZoneProps{
		ZoneName: fullDomainName,
		Comment:  jsii.String("Created by CDK for subdomain delegation from Cloudflare"),
	})

	// Add explicit dependency to ensure the check happens before zone creation
	hostedZone.Node().AddDependency(checkDnsResource)

	// Output the Route53 name servers to be used in Cloudflare DNS setup
	nameServers := hostedZone.HostedZoneNameServers()

	// Using Fn.join to properly handle CDK tokens
	nameServersString := awscdk.Fn_Join(jsii.String(", "), nameServers)

	awscdk.NewCfnOutput(stack, jsii.String("NameServers"), &awscdk.CfnOutputProps{
		Value:       nameServersString,
		Description: jsii.String("Name servers for the Route53 hosted zone. Add these as NS records in Cloudflare for delegation."),
	})

	// Store the hosted zone ID in SSM Parameter Store for reference
	paramName := props.Config.SsmParamPrefix + "/" + *props.Subdomain + "/" + strings.ReplaceAll(*props.ParentDomain, ".", "-") + "/hostedZoneId"
	ssmParam := awsssm.NewStringParameter(stack, jsii.String("HostedZoneIdSSMParam"), &awsssm.StringParameterProps{
		ParameterName: jsii.String(paramName),
		StringValue:   hostedZone.HostedZoneId(),
		Description:   jsii.String("Hosted Zone ID for " + *props.Subdomain + "." + *props.ParentDomain),
	})

	// Output the SSM parameter name
	awscdk.NewCfnOutput(stack, jsii.String("HostedZoneIdParamOutput"), &awscdk.CfnOutputProps{
		Value:       ssmParam.ParameterName(),
		Description: jsii.String("SSM Parameter containing the Hosted Zone ID"),
	})

	// Second custom resource: updates NS records after Route53 zone is ready
	updateNsResource := awscdk.NewCustomResource(stack, jsii.String("CloudflareDNSUpdater"), &awscdk.CustomResourceProps{
		ServiceToken: checkRecordsLambda.FunctionArn(),
		Properties: &map[string]interface{}{
			"Domain":      *props.ParentDomain,
			"Subdomain":   *props.Subdomain,
			"NameServers": nameServers,
			"SecretId":    cloudflareSecret.SecretName(),
			"Action":      "update", // Signal to Lambda to update NS records
		},
	})

	// Ensure the update only happens after the hosted zone is created
	updateNsResource.Node().AddDependency(hostedZone)

	// Return the stack and the hosted zone ID
	return stack, hostedZone.HostedZoneId()
}

// Separate stack for ACM certificate in us-east-1 (required for CloudFront)
type CertificateStackProps struct {
	awscdk.StackProps

	// Domain hosted on Cloudflare
	ParentDomain *string

	// Subdomain to be hosted on Route53
	Subdomain *string

	// Hosted Zone ID (direct reference, not from SSM)
	HostedZoneId *string

	// Configuration settings
	Config *ConfigFile
}

// Certificate stack for ACM certificate
func NewCertificateStack(scope constructs.Construct, id string, props *CertificateStackProps) awscdk.Stack {
	var sprops awscdk.StackProps
	if props != nil {
		sprops = props.StackProps
	}
	stack := awscdk.NewStack(scope, &id, &sprops)

	// Validate required properties
	if props.ParentDomain == nil || props.Subdomain == nil || props.HostedZoneId == nil || props.Config == nil {
		panic("ParentDomain, Subdomain, HostedZoneId and Config must be provided")
	}

	// Full domain name for the subdomain (e.g., sub.example.com)
	fullDomainName := jsii.String(*props.Subdomain + "." + *props.ParentDomain)

	// Import the Route53 hosted zone using the hosted zone ID
	importedZone := awsroute53.HostedZone_FromHostedZoneId(stack, jsii.String("ImportedZone"), props.HostedZoneId)

	certificate := awscertificatemanager.NewCertificate(stack, jsii.String("Certificate"), &awscertificatemanager.CertificateProps{
		DomainName: fullDomainName,
		Validation: awscertificatemanager.CertificateValidation_FromDns(importedZone),
	})

	// Store the certificate ARN in SSM Parameter Store for reference by other stacks
	certificateParamName := props.Config.SsmParamPrefix + "/" + *props.Subdomain + "/" + strings.ReplaceAll(*props.ParentDomain, ".", "-") + "/certificateArn"
	ssmParam := awsssm.NewStringParameter(stack, jsii.String("CertificateArnSSMParam"), &awsssm.StringParameterProps{
		ParameterName: jsii.String(certificateParamName),
		StringValue:   certificate.CertificateArn(),
		Description:   jsii.String("ACM Certificate ARN for " + *props.Subdomain + "." + *props.ParentDomain),
	})

	// Output the certificate ARN and SSM parameter name
	awscdk.NewCfnOutput(stack, jsii.String("CertificateArnOutput"), &awscdk.CfnOutputProps{
		Value:       certificate.CertificateArn(),
		Description: jsii.String("ACM Certificate ARN"),
	})

	awscdk.NewCfnOutput(stack, jsii.String("CertificateArnParamOutput"), &awscdk.CfnOutputProps{
		Value:       ssmParam.ParameterName(),
		Description: jsii.String("SSM Parameter containing the Certificate ARN"),
	})

	return stack
}

func main() {
	defer jsii.Close()

	// Create an app with cross-region references enabled through context
	app := awscdk.NewApp(&awscdk.AppProps{
		Context: &map[string]interface{}{
			"@aws-cdk/core:enableCrossAccountRegion": true,
		},
	})

	// Read the config.json file
	configBytes, err := os.ReadFile("config.json")
	if err != nil {
		panic("Failed to read config.json: " + err.Error())
	}

	// Parse the configuration
	var config ConfigFile
	if err := json.Unmarshal(configBytes, &config); err != nil {
		panic("Failed to parse config.json: " + err.Error())
	}

	// Set default regions if not provided
	mainRegion := "eu-north-1" // Default main region
	certRegion := "us-east-1"  // Default cert region (needed for CloudFront)
	if config.Regions != nil {
		if config.Regions.Main != "" {
			mainRegion = config.Regions.Main
		}
		if config.Regions.Certificate != "" {
			certRegion = config.Regions.Certificate
		}
	}

	// Domain configuration from config.json
	parentDomain := jsii.String(config.ParentDomain)
	subdomain := jsii.String(config.Subdomain)

	// Get secret name (default: "cftor53/cloudflare/api-token")
	secretName := "cftor53/cloudflare/api-token"
	if config.SecretName != "" {
		secretName = config.SecretName
	}

	// Get SSM parameter prefix (default: "/cftor53")
	ssmParamPrefix := "/cftor53"
	if config.SsmParamPrefix != "" {
		ssmParamPrefix = config.SsmParamPrefix
	}

	// Get Lambda settings with defaults
	lambdaTimeout := float64(120) // Default timeout: 120 seconds
	lambdaMemory := float64(256)  // Default memory: 256 MB
	if config.LambdaSettings != nil {
		if config.LambdaSettings.TimeoutSeconds > 0 {
			lambdaTimeout = float64(config.LambdaSettings.TimeoutSeconds)
		}
		if config.LambdaSettings.MemorySizeMB > 0 {
			lambdaMemory = float64(config.LambdaSettings.MemorySizeMB)
		}
	}

	// Create a secret in Secrets Manager for the Cloudflare API token (in the main region)
	secretsStack := awscdk.NewStack(app, jsii.String("CfCloudflareSecretsStack"), &awscdk.StackProps{
		Env: &awscdk.Environment{
			Region: jsii.String(mainRegion),
		},
		CrossRegionReferences: jsii.Bool(true),
	})

	// Create a secret for the Cloudflare API token
	cloudflareSecret := awssecretsmanager.NewSecret(secretsStack, jsii.String("CloudflareApiToken"), &awssecretsmanager.SecretProps{
		Description: jsii.String("Cloudflare API Token for DNS management"),
		SecretName:  jsii.String(secretName),
		SecretObjectValue: &map[string]awscdk.SecretValue{
			"api_token": awscdk.SecretValue_UnsafePlainText(jsii.String(config.ApiToken)),
		},
	})

	// Create the main stack with Route53 hosted zone and get the hosted zone ID
	_, hostedZoneId := NewCftor53Stack(app, "Cftor53Stack", &Cftor53StackProps{
		StackProps: awscdk.StackProps{
			CrossRegionReferences: jsii.Bool(true),
			Env: &awscdk.Environment{
				Region: jsii.String(mainRegion),
			},
		},
		ParentDomain:             parentDomain,
		Subdomain:                subdomain,
		CloudflareApiTokenSecret: cloudflareSecret,
		Config: &ConfigFile{
			SsmParamPrefix: ssmParamPrefix,
			LambdaSettings: &LambdaSettingsConfig{
				TimeoutSeconds: int(lambdaTimeout),
				MemorySizeMB:   int(lambdaMemory),
			},
			// Include the API token directly for cross-region deployments
			ApiToken: config.ApiToken,
		},
	})

	// Create the certificate stack in us-east-1 with direct reference to the hosted zone ID
	NewCertificateStack(app, "Cftor53CertificateStack", &CertificateStackProps{
		StackProps: awscdk.StackProps{
			Env: &awscdk.Environment{
				Region: jsii.String(certRegion),
			},
			CrossRegionReferences: jsii.Bool(true),
		},
		ParentDomain: parentDomain,
		Subdomain:    subdomain,
		HostedZoneId: hostedZoneId,
		Config: &ConfigFile{
			SsmParamPrefix: ssmParamPrefix,
			// Include the API token directly for cross-region deployments
			ApiToken: config.ApiToken,
		},
	})

	app.Synth(nil)
}
