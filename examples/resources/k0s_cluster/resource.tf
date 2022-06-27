resource "k0s_cluster" "example" {
  name    = "example"
  version = "1.23.8+k0s.0"

  hosts = [
    {
      role = "controller"

      ssh = {
        address  = "10.0.0.1"
        port     = 22
        user     = "root"
        key_path = "~/.ssh/id_ed25519.pub"
      }
    },
    {
      role = "worker"

      ssh = {
        address  = "10.0.0.2"
        port     = 22
        user     = "root"
        key_path = "~/.ssh/id_ed25519.pub"
      }
    }
  ]
}
