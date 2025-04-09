package main

import (
	"encoding/json"
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

// Local cloudflare package for DNS management
// Implementation located in lambda/main.go
type CloudflareDNSChecker interface {
	// Checks for colliding records and updates NS records
	// Returns a custom resource construct
	CheckAndUpdateCloudflareNS(scope constructs.Construct, id string, props *CloudflareDNSCheckerProps) constructs.Construct
}

type CloudflareDNSCheckerProps struct {
	// Domain in Cloudflare to check/update
	Domain *string

	// Subdomain to delegate
	Subdomain *string

	// Secret containing the Cloudflare API token
	ApiTokenSecret awssecretsmanager.ISecret

	// Route53 Name Servers
	NameServers *[]*string
}

// ConfigFile represents the structure of the config.json file
type ConfigFile struct {
	ApiToken string `json:"api_token"`
}

type Cftor53StackProps struct {
	awscdk.StackProps

	// Domain hosted on Cloudflare
	ParentDomain *string

	// Subdomain to be hosted on Route53
	Subdomain *string

	// Cloudflare API token secret
	CloudflareApiTokenSecret awssecretsmanager.ISecret
}

// Main stack for Route53 hosted zone
func NewCftor53Stack(scope constructs.Construct, id string, props *Cftor53StackProps) (awscdk.Stack, *string) {
	var sprops awscdk.StackProps
	if props != nil {
		sprops = props.StackProps
	}
	stack := awscdk.NewStack(scope, &id, &sprops)

	// Validate required properties
	if props.ParentDomain == nil || props.Subdomain == nil {
		panic("ParentDomain and Subdomain must be provided")
	}

	// Full domain name for the subdomain (e.g., sub.example.com)
	fullDomainName := jsii.String(*props.Subdomain + "." + *props.ParentDomain)

	// Create a custom resource to check for colliding DNS records in Cloudflare
	// but not make any changes yet
	checkRecordsLambda := awslambda.NewFunction(stack, jsii.String("CloudflareCheckDNSLambda"), &awslambda.FunctionProps{
		Runtime:      awslambda.Runtime_PROVIDED_AL2(),
		Handler:      jsii.String("bootstrap"),
		Code:         awslambda.Code_FromAsset(jsii.String("lambda/main.zip"), nil),
		Timeout:      awscdk.Duration_Seconds(jsii.Number(120)),
		MemorySize:   jsii.Number(256),
		Architecture: awslambda.Architecture_X86_64(),
	})

	// Grant permissions to read the Cloudflare API token secret
	// Create an explicit policy statement to grant read access to the secret
	secretArn := props.CloudflareApiTokenSecret.SecretArn()
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
			"SecretId":  props.CloudflareApiTokenSecret.SecretName(),
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
	paramName := "/cftor53/" + *props.Subdomain + "/" + strings.ReplaceAll(*props.ParentDomain, ".", "-") + "/hostedZoneId"
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
			"SecretId":    props.CloudflareApiTokenSecret.SecretName(),
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
}

func NewCertificateStack(scope constructs.Construct, id string, props *CertificateStackProps) awscdk.Stack {
	var sprops awscdk.StackProps
	if props != nil {
		sprops = props.StackProps
	}

	// Override region to us-east-1 for CloudFront compatibility
	sprops.Env = &awscdk.Environment{
		Account: sprops.Env.Account,
		Region:  jsii.String("us-east-1"),
	}

	stack := awscdk.NewStack(scope, &id, &sprops)

	// Validate required properties
	if props.ParentDomain == nil || props.Subdomain == nil || props.HostedZoneId == nil {
		panic("ParentDomain, Subdomain and HostedZoneId must be provided")
	}

	// Full domain name for the subdomain (e.g., sub.example.com)
	fullDomainName := jsii.String(*props.Subdomain + "." + *props.ParentDomain)

	// Import the hosted zone from the main stack using the direct ID
	hostedZone := awsroute53.HostedZone_FromHostedZoneId(stack, jsii.String("ImportedHostedZone"), props.HostedZoneId)

	// Create an SSL certificate for the subdomain
	certificate := awscertificatemanager.NewCertificate(stack, jsii.String("SubdomainCertificate"), &awscertificatemanager.CertificateProps{
		DomainName: fullDomainName,
		Validation: awscertificatemanager.CertificateValidation_FromDns(hostedZone),
	})

	// Store the certificate ARN in SSM Parameter Store
	certificateParamName := "/cftor53/" + *props.Subdomain + "/" + strings.ReplaceAll(*props.ParentDomain, ".", "-") + "/certificateArn"
	certParam := awsssm.NewStringParameter(stack, jsii.String("CertificateArnSSMParam"), &awsssm.StringParameterProps{
		ParameterName: jsii.String(certificateParamName),
		StringValue:   certificate.CertificateArn(),
		Description:   jsii.String("Certificate ARN for " + *props.Subdomain + "." + *props.ParentDomain),
	})

	// Output the certificate ARN
	awscdk.NewCfnOutput(stack, jsii.String("CertificateArnOutput"), &awscdk.CfnOutputProps{
		Value:       certificate.CertificateArn(),
		Description: jsii.String("Certificate ARN to use with services like CloudFront or ALB"),
	})

	// Output the SSM parameter name
	awscdk.NewCfnOutput(stack, jsii.String("CertificateArnParamOutput"), &awscdk.CfnOutputProps{
		Value:       certParam.ParameterName(),
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

	// Domain configuration
	parentDomain := jsii.String("fuk.fi") // Replace with your Cloudflare-hosted domain
	subdomain := jsii.String("yakv")      // Replace with your desired subdomain

	// Read the config.json file to get the Cloudflare API token
	configJsonBytes := []byte(`{"api_token": "iT23_YRf8Yv-Jo3je06E7iXOxrlbYsWYAd3jYfNw"}`)
	var config ConfigFile
	if err := json.Unmarshal(configJsonBytes, &config); err != nil {
		panic("Failed to parse config.json: " + err.Error())
	}

	// Create a secret in Secrets Manager for the Cloudflare API token
	mainStack := awscdk.NewStack(app, jsii.String("CfCloudflareSecretsStack"), nil)

	// Create a secret for the Cloudflare API token
	cloudflareSecret := awssecretsmanager.NewSecret(mainStack, jsii.String("CloudflareApiToken"), &awssecretsmanager.SecretProps{
		Description: jsii.String("Cloudflare API Token for DNS management"),
		SecretName:  jsii.String("cftor53/cloudflare/api-token"),
		SecretObjectValue: &map[string]awscdk.SecretValue{
			"api_token": awscdk.SecretValue_UnsafePlainText(jsii.String(config.ApiToken)),
		},
	})

	// Create the main stack with Route53 hosted zone and get the hosted zone ID
	_, hostedZoneId := NewCftor53Stack(app, "Cftor53Stack", &Cftor53StackProps{
		StackProps: awscdk.StackProps{
			CrossRegionReferences: jsii.Bool(true),
		},
		ParentDomain:             parentDomain,
		Subdomain:                subdomain,
		CloudflareApiTokenSecret: cloudflareSecret,
	})

	// Create the certificate stack in us-east-1 with direct reference to the hosted zone ID
	NewCertificateStack(app, "Cftor53CertificateStack", &CertificateStackProps{
		StackProps: awscdk.StackProps{
			Env: &awscdk.Environment{
				Region: jsii.String("us-east-1"), // Certificate must be in us-east-1 for CloudFront
			},
			CrossRegionReferences: jsii.Bool(true),
		},
		ParentDomain: parentDomain,
		Subdomain:    subdomain,
		HostedZoneId: hostedZoneId,
	})

	app.Synth(nil)
}
