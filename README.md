# Terraform provider for k0s (using embedded k0sctl)

> ⚠️ The project has been archived because I've migrated my production and development workloads to Talos.

> ⚠️ The provider name on Terraform registry has been renamed from `adnsio/k0s` to `alessiodionisi/k0s`, please update your `required_providers` block.

Terraform provider to create and manage [k0s](https://k0sproject.io) Kubernetes clusters, using embedded [k0sctl](https://github.com/k0sproject/k0sctl).

## Getting started

You can install stable releases of the provider from the [Terrafom registry](https://registry.terraform.io/providers/alessiodionisi/k0s/latest).

## Contributing

If you wish to work on the provider, you'll first need these requirements:

- [Terraform](https://www.terraform.io) >= 1.0
- [Go](https://golang.org) >= 1.21
- [Task](https://taskfile.dev) >= 1.3

To compile the provider, run `task install`. This will build the provider and put the provider binary in the `$GOPATH/bin` directory.

To generate or update documentation, run `task generate`.

In order to run the full suite of acceptance tests, run `task acceptance-tests`.

_Note:_ acceptance tests create real resources, and often cost money to run.
