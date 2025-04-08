package main

import (
	"strings"

	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awscertificatemanager"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsroute53"
	"github.com/aws/constructs-go/constructs/v10"
	"github.com/aws/jsii-runtime-go"
)

type Cftor53StackProps struct {
	awscdk.StackProps

	// Domain hosted on Cloudflare
	ParentDomain *string

	// Subdomain to be hosted on Route53
	Subdomain *string
}

// Main stack for Route53 hosted zone
func NewCftor53Stack(scope constructs.Construct, id string, props *Cftor53StackProps) awscdk.Stack {
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

	// Create a Route53 hosted zone for the subdomain
	hostedZone := awsroute53.NewPublicHostedZone(stack, jsii.String("SubdomainHostedZone"), &awsroute53.PublicHostedZoneProps{
		ZoneName: fullDomainName,
		Comment:  jsii.String("Created by CDK for subdomain delegation from Cloudflare"),
	})

	// Output the Route53 name servers to be used in Cloudflare DNS setup
	nameServers := hostedZone.HostedZoneNameServers()

	// Using Fn.join instead of Go's strings.Join to properly handle CDK tokens
	nameServersString := awscdk.Fn_Join(jsii.String(", "), nameServers)

	awscdk.NewCfnOutput(stack, jsii.String("NameServers"), &awscdk.CfnOutputProps{
		Value:       nameServersString,
		Description: jsii.String("Name servers for the Route53 hosted zone. Add these as NS records in Cloudflare for delegation."),
	})

	// Output the hosted zone ID for cross-stack reference
	exportName := "HostedZoneId-" + *props.Subdomain + "-" + strings.ReplaceAll(*props.ParentDomain, ".", "-")
	awscdk.NewCfnOutput(stack, jsii.String("HostedZoneId"), &awscdk.CfnOutputProps{
		Value:       hostedZone.HostedZoneId(),
		Description: jsii.String("The Hosted Zone ID for the subdomain"),
		ExportName:  jsii.String(exportName),
	})

	return stack
}

// Separate stack for ACM certificate in us-east-1 (required for CloudFront)
type CertificateStackProps struct {
	awscdk.StackProps

	// Domain hosted on Cloudflare
	ParentDomain *string

	// Subdomain to be hosted on Route53
	Subdomain *string

	// Hosted Zone ID for DNS validation
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

	// Import the hosted zone from the main stack
	hostedZone := awsroute53.HostedZone_FromHostedZoneId(stack, jsii.String("ImportedHostedZone"), props.HostedZoneId)

	// Create an SSL certificate for the subdomain
	certificate := awscertificatemanager.NewCertificate(stack, jsii.String("SubdomainCertificate"), &awscertificatemanager.CertificateProps{
		DomainName: fullDomainName,
		Validation: awscertificatemanager.CertificateValidation_FromDns(hostedZone),
	})

	// Output the certificate ARN
	certificateExportName := "CertificateArn-" + *props.Subdomain + "-" + strings.ReplaceAll(*props.ParentDomain, ".", "-")
	awscdk.NewCfnOutput(stack, jsii.String("CertificateArn"), &awscdk.CfnOutputProps{
		Value:       certificate.CertificateArn(),
		Description: jsii.String("Certificate ARN to use with services like CloudFront or ALB"),
		ExportName:  jsii.String(certificateExportName),
	})

	return stack
}

func main() {
	defer jsii.Close()

	app := awscdk.NewApp(nil)

	// Domain configuration
	parentDomain := jsii.String("fuk.fi") // Replace with your Cloudflare-hosted domain
	subdomain := jsii.String("yakv")      // Replace with your desired subdomain

	// Create the main stack with Route53 hosted zone
	NewCftor53Stack(app, "Cftor53Stack", &Cftor53StackProps{
		StackProps: awscdk.StackProps{
			Env: env(),
		},
		ParentDomain: parentDomain,
		Subdomain:    subdomain,
	})

	// Create the certificate stack in us-east-1
	// We can't directly reference the hosted zone output here, so we need to use cross-stack references
	importName := "HostedZoneId-" + *subdomain + "-" + strings.ReplaceAll(*parentDomain, ".", "-")
	NewCertificateStack(app, "Cftor53CertificateStack", &CertificateStackProps{
		StackProps: awscdk.StackProps{
			Env: env(),
		},
		ParentDomain: parentDomain,
		Subdomain:    subdomain,
		HostedZoneId: awscdk.Fn_ImportValue(jsii.String(importName)),
	})

	app.Synth(nil)
}

// env determines the AWS environment (account+region) in which our stack is to
// be deployed. For more information see: https://docs.aws.amazon.com/cdk/latest/guide/environments.html
func env() *awscdk.Environment {
	// If you want your stack to be environment-agnostic, use this option
	// return nil

	// Uncomment below to use specific account/region
	return &awscdk.Environment{
		Account: jsii.String("867344475763"), // Replace with your AWS account ID
		Region:  jsii.String("eu-north-1"),   // Replace with your preferred region
	}

	// Uncomment below to use the account/region from CDK_DEFAULT_* environment variables
	// return &awscdk.Environment{
	// 	Account: jsii.String(os.Getenv("CDK_DEFAULT_ACCOUNT")),
	// 	Region:  jsii.String(os.Getenv("CDK_DEFAULT_REGION")),
	// }
}
