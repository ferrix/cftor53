# CloudFlare to Route53 Subdomain Delegation

This CDK project sets up a Route53 hosted zone for a subdomain of a domain hosted on Cloudflare, allowing you to serve the subdomain from AWS while keeping the parent domain on Cloudflare.

## Architecture

The stack creates:
1. A Route53 public hosted zone for your subdomain
2. A DNS-validated ACM certificate for the subdomain (in us-east-1 for CloudFront compatibility)
3. CloudFormation outputs with the name servers to add to Cloudflare

## Prerequisites

- AWS account and configured AWS CLI
- AWS CDK installed (`npm install -g aws-cdk`)
- Go 1.18 or later
- Domain registered and managed in Cloudflare

## Deployment Steps

1. Clone this repository
2. Update the `main()` function in `cftor53.go` with your domain details:
   ```go
   NewCftor53Stack(app, "Cftor53Stack", &Cftor53StackProps{
       StackProps: awscdk.StackProps{
           Env: env(),
       },
       ParentDomain: jsii.String("example.com"), // Replace with your Cloudflare-hosted domain
       Subdomain:    jsii.String("sub"),         // Replace with your desired subdomain
   })
   ```

3. Deploy the stack:
   ```bash
   cdk deploy
   ```

4. After deployment, note the name servers in the CloudFormation outputs.

5. In Cloudflare DNS settings for your domain, add NS records for your subdomain pointing to the Route53 name servers:
   - Type: NS
   - Name: your-subdomain
   - Content: the name servers from the CloudFormation outputs (add each as a separate record)
   - TTL: Auto

## Using the Certificate

The stack outputs the ARN of the generated certificate, which you can use with:
- CloudFront distributions
- API Gateway custom domains
- Application Load Balancers
- Other AWS services that support custom domains

## Troubleshooting

- **DNS Propagation**: DNS changes can take 24-48 hours to fully propagate worldwide.
- **Certificate Validation**: The certificate validation will automatically complete once the DNS records are properly set up.
- **Multiple Subdomains**: For multiple subdomains, deploy separate stacks with different subdomain parameters.

## Security

- This setup enables secure HTTPS for your subdomain using AWS ACM.
- Ensure your AWS IAM permissions follow the principle of least privilege.
