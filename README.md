# Terraform provider for k0s (using k0sctl lib)

> ⚠️ The provider name on Terraform registry has been renamed from `adnsio/k0s` to `alessiodionisi/k0s`, please update your `required_providers` block.

Terraform provider to create and manage [k0s](https://k0sproject.io) Kubernetes clusters, using embedded [k0sctl](https://github.com/k0sproject/k0sctl).

Docs: https://registry.terraform.io/providers/alessiodionisi/k0s/latest/docs

## Requirements

- [Terraform](https://www.terraform.io/downloads.html) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.20

## Building The Provider

1. Clone the repository
1. Enter the repository directory
1. Build the provider using the Go `install` command:

```shell
go install
```
