# authing_custom_domain

Manages the **single** custom domain of the configured Authing user pool. Creation refuses to adopt any existing domain, even one with the same name; import it first. Deleting this resource removes the domain from the pool. Changes to the domain name require replacement; review the disruption before applying.

The resource reads DNS TXT/CNAME records and verification flags, but intentionally does **not** expose `httpsCertificate` or `httpsPrivateKey`. Marking a field Sensitive would still store it in Terraform state, so TLS certificate installation/update is outside this resource. The provider does not manage DNS records or HTTPS certificate updates. Do not enable HTTP response-body debug logging when handling certificate-bearing API responses.

```hcl
resource "authing_custom_domain" "login" {
  custom_domain = "sso.example.com"
}

output "custom_domain_dns_txt" {
  value = {
    name  = authing_custom_domain.login.dns_txt_name
    value = authing_custom_domain.login.dns_txt_value
  }
}
```

## Arguments

- `custom_domain` (Required, ForceNew): Custom domain name.

## Read-only attributes

- `id`: Domain name.
- `dns_txt_name`, `dns_txt_value`: DNS TXT verification record.
- `dns_verified`: DNS verification status.
- `cname`: DNS CNAME target.
- `https_verified`: HTTPS verification status.

## Import

Import using the exact custom domain name in the configured user pool:

```sh
terraform import authing_custom_domain.login sso.example.com
```

This singleton API has no domain parameter on GET or remove. The provider compares the remote domain name to state before removal and refuses to delete a replacement domain. Only explicit 404 removes state on read. If creation succeeds remotely but readback fails, check Authing and import the domain rather than retrying creation.
