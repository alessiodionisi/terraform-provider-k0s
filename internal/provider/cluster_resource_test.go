package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccClusterResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccClusterResourceConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("k0s_cluster.test", "id", "test-id"),
					resource.TestCheckResourceAttr("k0s_cluster.test", "name", "test-cluster"),
				),
			},
			// ImportState testing
			// {
			// 	ResourceName:      "k0s_cluster.test",
			// 	ImportState:       true,
			// 	ImportStateVerify: true,
			// 	// This is not normally necessary, but is here because this
			// 	// example code does not have an actual upstream service.
			// 	// Once the Read method is able to refresh information from
			// 	// the upstream service, this can be removed.
			// 	// ImportStateVerifyIgnore: []string{"configurable_attribute"},
			// },
			// // Update and Read testing
			// {
			// 	Config: testAccClusterResourceConfig(),
			// 	Check: resource.ComposeAggregateTestCheckFunc(
			// 		resource.TestCheckResourceAttr("k0s_cluster.test", "configurable_attribute", "two"),
			// 	),
			// },
			// Delete testing automatically occurs in TestCase
		},
	})
}

func testAccClusterResourceConfig() string {
	return fmt.Sprintf(`
resource "k0s_cluster" "test" {
  name = "test"
	version = "1.27.2+k0s.0"

	hosts = [
		{
			role = "controller+worker"

      ssh = {
        address  = "127.0.0.1"
        port     = 22
        user     = "root"
        key_path = "~/.ssh/id_ed25519.pub"
      }
		}
	]
}
`)
}
