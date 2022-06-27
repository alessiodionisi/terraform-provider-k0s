package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	k0sctl_phase "github.com/k0sproject/k0sctl/phase"
	k0sctl_v1beta1 "github.com/k0sproject/k0sctl/pkg/apis/k0sctl.k0sproject.io/v1beta1"
	k0sctl_cluster "github.com/k0sproject/k0sctl/pkg/apis/k0sctl.k0sproject.io/v1beta1/cluster"
	k0s_rig "github.com/k0sproject/rig"
)

var _ tfsdk.ResourceType = clusterResourceType{}
var _ tfsdk.Resource = clusterResource{}
var _ tfsdk.ResourceWithImportState = clusterResource{}

type clusterResourceType struct{}

func (t clusterResourceType) GetSchema(ctx context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		MarkdownDescription: "Manages a k0s cluster.",
		Attributes: map[string]tfsdk.Attribute{
			"id": {
				MarkdownDescription: "Unique ID of the cluster.",
				Computed:            true,
				Type:                types.StringType,
				PlanModifiers: tfsdk.AttributePlanModifiers{
					tfsdk.UseStateForUnknown(),
				},
			},
			"name": {
				MarkdownDescription: "Name of the cluster.",
				Required:            true,
				Type:                types.StringType,
			},
			"version": {
				MarkdownDescription: "Desired k0s version.",
				Required:            true,
				Type:                types.StringType,
			},
			"hosts": {
				MarkdownDescription: "Hosts configuration.",
				Required:            true,
				Attributes: tfsdk.SetNestedAttributes(map[string]tfsdk.Attribute{
					"role": {
						MarkdownDescription: "Role of the host. One of `controller`, `controller+worker`, `single`, `worker`.",
						Required:            true,
						Type:                types.StringType,
					},
					"no_taints": {
						MarkdownDescription: "When `true` and used in conjuction with the `controller+worker` role, the default taints are disabled making regular workloads schedulable on the node. By default, k0s sets a node-role.kubernetes.io/master:NoSchedule taint on `controller+worker` nodes and only workloads with toleration for it will be scheduled.",
						Optional:            true,
						Type:                types.BoolType,
					},
					"hostname": {
						MarkdownDescription: "Override host's hostname. When not set, the hostname reported by the operating system is used.",
						Optional:            true,
						Type:                types.StringType,
					},
					"ssh": {
						MarkdownDescription: "SSH connection options.",
						Required:            true,
						Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
							"address": {
								MarkdownDescription: "IP address of the host.",
								Required:            true,
								Type:                types.StringType,
							},
							"user": {
								MarkdownDescription: "Username to log in as.",
								Required:            true,
								Type:                types.StringType,
							},
							"port": {
								MarkdownDescription: "TCP port of the SSH service on the host.",
								Required:            true,
								Type:                types.Int64Type,
							},
							"key_path": {
								MarkdownDescription: "Path to an SSH private key file.",
								Required:            true,
								Type:                types.StringType,
							},
						}),
					},
				}),
			},
			"kubeconfig": {
				MarkdownDescription: "Admin kubeconfig of the cluster.",
				Computed:            true,
				Type:                types.StringType,
				Sensitive:           true,
			},
		},
	}, nil
}

func (t clusterResourceType) NewResource(ctx context.Context, in tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	provider, diags := convertProviderType(in)

	return clusterResource{
		provider: provider,
	}, diags
}

type clusterResourceDataHostSSH struct {
	Address types.String `tfsdk:"address"`
	User    types.String `tfsdk:"user"`
	Port    types.Int64  `tfsdk:"port"`
	KeyPath types.String `tfsdk:"key_path"`
}

type clusterResourceDataHost struct {
	Role     types.String               `tfsdk:"role"`
	NoTaints types.Bool                 `tfsdk:"no_taints"`
	Hostname types.String               `tfsdk:"hostname"`
	SSH      clusterResourceDataHostSSH `tfsdk:"ssh"`
}

type clusterResourceData struct {
	ID         types.String              `tfsdk:"id"`
	Name       types.String              `tfsdk:"name"`
	Version    types.String              `tfsdk:"version"`
	Hosts      []clusterResourceDataHost `tfsdk:"hosts"`
	Kubeconfig types.String              `tfsdk:"kubeconfig"`
}

type clusterResource struct {
	provider provider
}

func (r clusterResource) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	var data clusterResourceData

	diags := req.Config.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	k0sctlConfig := getK0sctlConfig(data)

	if err := k0sctlConfig.Validate(); err != nil {
		resp.Diagnostics.AddError("k0sctl Error", fmt.Sprintf("Unable to create cluster, got error: %s", err))
		return
	}

	manager := getK0sctlManagerForCreateOrUpdate(k0sctlConfig)

	if err := manager.Run(); err != nil {
		resp.Diagnostics.AddError("k0sctl Error", fmt.Sprintf("Unable to create cluster, got error: %s", err))
		return
	}

	data.ID = types.String{Value: data.Name.Value}
	data.Kubeconfig = types.String{Value: k0sctlConfig.Metadata.Kubeconfig}

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

func (r clusterResource) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var data clusterResourceData

	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	k0sctlConfig := getK0sctlConfig(data)

	if err := k0sctlConfig.Validate(); err != nil {
		resp.Diagnostics.AddError("k0sctl Error", fmt.Sprintf("Unable to read cluster, got error: %s", err))
		return
	}

	k0sctlConfig.Spec.Hosts = k0sctl_cluster.Hosts{k0sctlConfig.Spec.K0sLeader()}

	manager := k0sctl_phase.Manager{
		Config: k0sctlConfig,
	}

	manager.AddPhase(
		&k0sctl_phase.Connect{},
		&k0sctl_phase.DetectOS{},
		&k0sctl_phase.GatherK0sFacts{},
		&k0sctl_phase.GetKubeconfig{},
		&k0sctl_phase.Disconnect{},
	)

	if err := manager.Run(); err != nil {
		resp.Diagnostics.AddError("k0sctl Error", fmt.Sprintf("Unable to read cluster, got error: %s", err))
		return
	}

	data.Kubeconfig = types.String{Value: k0sctlConfig.Metadata.Kubeconfig}

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

func (r clusterResource) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var data clusterResourceData

	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	k0sctlConfig := getK0sctlConfig(data)

	if err := k0sctlConfig.Validate(); err != nil {
		resp.Diagnostics.AddError("k0sctl Error", fmt.Sprintf("Unable to update cluster, got error: %s", err))
		return
	}

	manager := getK0sctlManagerForCreateOrUpdate(k0sctlConfig)

	if err := manager.Run(); err != nil {
		resp.Diagnostics.AddError("k0sctl Error", fmt.Sprintf("Unable to update cluster, got error: %s", err))
		return
	}

	data.Kubeconfig = types.String{Value: k0sctlConfig.Metadata.Kubeconfig}

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
}

func (r clusterResource) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var data clusterResourceData

	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	k0sctlConfig := getK0sctlConfig(data)

	if err := k0sctlConfig.Validate(); err != nil {
		resp.Diagnostics.AddError("k0sctl Error", fmt.Sprintf("Unable to delete cluster, got error: %s", err))
		return
	}

	manager := k0sctl_phase.Manager{
		Config: k0sctlConfig,
	}

	lockPhase := &k0sctl_phase.Lock{}

	manager.AddPhase(
		&k0sctl_phase.Connect{},
		&k0sctl_phase.DetectOS{},
		lockPhase,
		&k0sctl_phase.PrepareHosts{},
		&k0sctl_phase.GatherK0sFacts{},
		&k0sctl_phase.Reset{},
		&k0sctl_phase.Unlock{Cancel: lockPhase.Cancel},
		&k0sctl_phase.Disconnect{},
	)

	if err := manager.Run(); err != nil {
		resp.Diagnostics.AddError("k0sctl Error", fmt.Sprintf("Unable to delete cluster, got error: %s", err))
		return
	}
}

func (r clusterResource) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	tfsdk.ResourceImportStatePassthroughID(ctx, tftypes.NewAttributePath().WithAttributeName("id"), req, resp)
}

func getK0sctlManagerForCreateOrUpdate(k0sctlConfig *k0sctl_v1beta1.Cluster) k0sctl_phase.Manager {
	manager := k0sctl_phase.Manager{
		Config: k0sctlConfig,
	}

	lockPhase := &k0sctl_phase.Lock{}

	manager.AddPhase(
		&k0sctl_phase.Connect{},
		&k0sctl_phase.DetectOS{},
		lockPhase,
		&k0sctl_phase.PrepareHosts{},
		&k0sctl_phase.GatherFacts{},
		&k0sctl_phase.DownloadBinaries{},
		&k0sctl_phase.UploadFiles{},
		&k0sctl_phase.ValidateHosts{},
		&k0sctl_phase.GatherK0sFacts{},
		&k0sctl_phase.ValidateFacts{},
		&k0sctl_phase.UploadBinaries{},
		&k0sctl_phase.DownloadK0s{},
		&k0sctl_phase.PrepareArm{},
		&k0sctl_phase.ConfigureK0s{},
		&k0sctl_phase.InitializeK0s{},
		&k0sctl_phase.InstallControllers{},
		&k0sctl_phase.InstallWorkers{},
		&k0sctl_phase.UpgradeControllers{},
		&k0sctl_phase.UpgradeWorkers{},
		&k0sctl_phase.Unlock{Cancel: lockPhase.Cancel},
		&k0sctl_phase.GetKubeconfig{},
		&k0sctl_phase.Disconnect{},
	)

	return manager
}

func getK0sctlConfig(data clusterResourceData) *k0sctl_v1beta1.Cluster {
	k0sctlHosts := []*k0sctl_cluster.Host{}

	for _, host := range data.Hosts {
		k0sctlHosts = append(k0sctlHosts, &k0sctl_cluster.Host{
			Connection: k0s_rig.Connection{
				SSH: &k0s_rig.SSH{
					Address: host.SSH.Address.Value,
					Port:    int(host.SSH.Port.Value),
					User:    host.SSH.User.Value,
				},
			},
			Role:             host.Role.Value,
			NoTaints:         host.NoTaints.Value,
			HostnameOverride: host.Hostname.Value,
		})
	}

	return &k0sctl_v1beta1.Cluster{
		APIVersion: "k0sctl.k0sproject.io/v1beta1",
		Kind:       "Cluster",
		Metadata: &k0sctl_v1beta1.ClusterMetadata{
			Name: data.Name.Value,
		},
		Spec: &k0sctl_cluster.Spec{
			Hosts: k0sctlHosts,
			K0s: &k0sctl_cluster.K0s{
				Version: data.Version.Value,
			},
		},
	}
}
