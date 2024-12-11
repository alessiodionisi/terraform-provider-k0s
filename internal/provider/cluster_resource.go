package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/k0sproject/dig"
	k0sctl_phase "github.com/k0sproject/k0sctl/phase"
	k0sctl_v1beta1 "github.com/k0sproject/k0sctl/pkg/apis/k0sctl.k0sproject.io/v1beta1"
	k0sctl_cluster "github.com/k0sproject/k0sctl/pkg/apis/k0sctl.k0sproject.io/v1beta1/cluster"
	k0s_rig "github.com/k0sproject/rig"
	"github.com/k0sproject/version"
	"gopkg.in/yaml.v2"
)

var _ resource.Resource = &ClusterResource{}
var _ resource.ResourceWithImportState = &ClusterResource{}

type ClusterResourceModel struct {
	ID            types.String               `tfsdk:"id"`
	Name          types.String               `tfsdk:"name"`
	Version       types.String               `tfsdk:"version"`
	Hosts         []ClusterResourceModelHost `tfsdk:"hosts"`
	Kubeconfig    types.String               `tfsdk:"kubeconfig"`
	DynamicConfig types.Bool                 `tfsdk:"dynamic_config"`
	Config        types.String               `tfsdk:"config"`
	Concurrency   types.Int64                `tfsdk:"concurrency"`
	NoWait        types.Bool                 `tfsdk:"no_wait"`
	NoDrain       types.Bool                 `tfsdk:"no_drain"`
}

type ClusterResourceModelHost struct {
	Role             types.String                `tfsdk:"role"`
	NoTaints         types.Bool                  `tfsdk:"no_taints"`
	Hostname         types.String                `tfsdk:"hostname"`
	SSH              ClusterResourceModelHostSSH `tfsdk:"ssh"`
	PrivateInterface types.String                `tfsdk:"private_interface"`
	PrivateAddress   types.String                `tfsdk:"private_address"`
	OS               types.String                `tfsdk:"os"`
	InstallFlags     types.List                  `tfsdk:"install_flags"`
	Environment      types.Map                   `tfsdk:"environment"`
}

type ClusterResourceModelHostSSH struct {
	Address types.String `tfsdk:"address"`
	User    types.String `tfsdk:"user"`
	Port    types.Int64  `tfsdk:"port"`
	KeyPath types.String `tfsdk:"key_path"`
}

type ClusterResource struct {
	provider K0sProvider
}

func (r *ClusterResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cluster"
}

func (r *ClusterResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a k0s cluster.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique ID of the cluster.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the cluster.",
				Required:            true,
			},
			"version": schema.StringAttribute{
				MarkdownDescription: "Desired k0s version.",
				Required:            true,
			},
			"dynamic_config": schema.BoolAttribute{
				MarkdownDescription: "Enable k0s dynamic config.",
				Optional:            true,
			},
			"config": schema.StringAttribute{
				MarkdownDescription: "Embedded k0s cluster configuration. When left out, the output of `k0s config create` will be used.",
				Optional:            true,
			},
			"hosts": schema.SetNestedAttribute{
				MarkdownDescription: "Hosts configuration.",
				Required:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"role": schema.StringAttribute{
							MarkdownDescription: "Role of the host. One of `controller`, `controller+worker`, `single`, `worker`.",
							Required:            true,
						},
						"no_taints": schema.BoolAttribute{
							MarkdownDescription: "When `true` and used in conjuction with the `controller+worker` role, the default taints are disabled making regular workloads schedulable on the node. By default, k0s sets a `node-role.kubernetes.io/master:NoSchedule` taint on `controller+worker` nodes and only workloads with toleration for it will be scheduled.",
							Optional:            true,
						},
						"hostname": schema.StringAttribute{
							MarkdownDescription: "Override host's hostname. When not set, the hostname reported by the operating system is used.",
							Optional:            true,
						},
						"private_interface": schema.StringAttribute{
							MarkdownDescription: "Override private network interface selected by host fact gathering. Useful in case fact gathering picks the wrong private network interface.",
							Optional:            true,
						},
						"private_address": schema.StringAttribute{
							MarkdownDescription: "Override private IP address selected by host fact gathering.",
							Optional:            true,
						},
						"os": schema.StringAttribute{
							MarkdownDescription: "Override OS distribution auto-detection.",
							Optional:            true,
						},
						"install_flags": schema.ListAttribute{
							MarkdownDescription: "Extra flags passed to the `k0s install` command on the target host.",
							Optional:            true,
							ElementType:         types.StringType,
						},
						"environment": schema.MapAttribute{
							MarkdownDescription: "List of key-value pairs to set to the target host's environment variables.",
							Optional:            true,
							ElementType:         types.StringType,
						},
						"ssh": schema.SingleNestedAttribute{
							MarkdownDescription: "SSH connection options.",
							Required:            true,
							Attributes: map[string]schema.Attribute{
								"address": schema.StringAttribute{
									MarkdownDescription: "IP address of the host.",
									Required:            true,
								},
								"user": schema.StringAttribute{
									MarkdownDescription: "Username to log in as.",
									Optional:            true,
								},
								"port": schema.Int64Attribute{
									MarkdownDescription: "TCP port of the SSH service on the host.",
									Required:            true,
								},
								"key_path": schema.StringAttribute{
									MarkdownDescription: "Path to an SSH private key file.",
									Optional:            true,
								},
							},
						},
					},
				},
			},
			"concurrency": schema.Int64Attribute{
				MarkdownDescription: "Maximum number of hosts to configure in parallel. If omitted, the default value is `0` (unlimited).",
				Optional:            true,
			},
			"no_wait": schema.BoolAttribute{
				MarkdownDescription: "Do not wait for worker nodes to join. If omitted, the default value is `false`.",
				Optional:            true,
			},
			"no_drain": schema.BoolAttribute{
				MarkdownDescription: "Do not drain worker nodes when upgrading. If omitted, the default value is `false`.",
				Optional:            true,
			},
			"kubeconfig": schema.StringAttribute{
				MarkdownDescription: "Admin kubeconfig of the cluster.",
				Computed:            true,
				Sensitive:           true,
			},
		},
	}
}

func (r *ClusterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *ClusterResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	k0sctlConfig := getK0sctlConfig(ctx, &resp.Diagnostics, data)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := k0sctlConfig.Validate(); err != nil {
		resp.Diagnostics.AddError("k0sctl Error", fmt.Sprintf("Unable to create cluster, got error: %s", err))
		return
	}

	manager := getK0sctlManagerForCreateOrUpdate(data, k0sctlConfig)

	if err := manager.Run(); err != nil {
		resp.Diagnostics.AddError("k0sctl Error", fmt.Sprintf("Unable to create cluster, got error: %s", err))
		return
	}

	data.ID = types.StringValue(data.Name.ValueString())
	data.Kubeconfig = types.StringValue(k0sctlConfig.Metadata.Kubeconfig)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ClusterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *ClusterResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	k0sctlConfig := getK0sctlConfig(ctx, &resp.Diagnostics, data)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := k0sctlConfig.Validate(); err != nil {
		resp.Diagnostics.AddError("k0sctl Error", fmt.Sprintf("Unable to read cluster, got error: %s", err))
		return
	}

	k0sctlConfig.Spec.Hosts = k0sctl_cluster.Hosts{k0sctlConfig.Spec.K0sLeader()}

	manager := k0sctl_phase.Manager{
		Config:      k0sctlConfig,
		Concurrency: int(data.Concurrency.ValueInt64()),
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

	data.Kubeconfig = types.StringValue(k0sctlConfig.Metadata.Kubeconfig)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ClusterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *ClusterResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	k0sctlConfig := getK0sctlConfig(ctx, &resp.Diagnostics, data)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := k0sctlConfig.Validate(); err != nil {
		resp.Diagnostics.AddError("k0sctl Error", fmt.Sprintf("Unable to update cluster, got error: %s", err))
		return
	}

	manager := getK0sctlManagerForCreateOrUpdate(data, k0sctlConfig)

	if err := manager.Run(); err != nil {
		resp.Diagnostics.AddError("k0sctl Error", fmt.Sprintf("Unable to update cluster, got error: %s", err))
		return
	}

	data.Kubeconfig = types.StringValue(k0sctlConfig.Metadata.Kubeconfig)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ClusterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *ClusterResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	k0sctlConfig := getK0sctlConfig(ctx, &resp.Diagnostics, data)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := k0sctlConfig.Validate(); err != nil {
		resp.Diagnostics.AddError("k0sctl Error", fmt.Sprintf("Unable to delete cluster, got error: %s", err))
		return
	}

	manager := k0sctl_phase.Manager{
		Config:      k0sctlConfig,
		Concurrency: int(data.Concurrency.ValueInt64()),
	}

	lockPhase := &k0sctl_phase.Lock{}

	manager.AddPhase(
		&k0sctl_phase.Connect{},
		&k0sctl_phase.DetectOS{},
		lockPhase,
		&k0sctl_phase.PrepareHosts{},
		&k0sctl_phase.GatherK0sFacts{},
		&k0sctl_phase.ResetWorkers{
			NoDrain:  true,
			NoDelete: true,
		},
		&k0sctl_phase.ResetControllers{
			NoDrain:  true,
			NoDelete: true,
			NoLeave:  true,
		},
		&k0sctl_phase.ResetLeader{},
		&k0sctl_phase.Unlock{Cancel: lockPhase.Cancel},
		&k0sctl_phase.Disconnect{},
	)

	if err := manager.Run(); err != nil {
		resp.Diagnostics.AddError("k0sctl Error", fmt.Sprintf("Unable to delete cluster, got error: %s", err))
		return
	}
}

func (r *ClusterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *ClusterResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
}

func getK0sctlManagerForCreateOrUpdate(data *ClusterResourceModel, k0sctlConfig *k0sctl_v1beta1.Cluster) k0sctl_phase.Manager {
	k0sctl_phase.NoWait = data.NoWait.ValueBool()

	manager := k0sctl_phase.Manager{
		Config:      k0sctlConfig,
		Concurrency: int(data.Concurrency.ValueInt64()),
	}

	lockPhase := &k0sctl_phase.Lock{}

	manager.AddPhase(
		&k0sctl_phase.DefaultK0sVersion{},
		&k0sctl_phase.Connect{},
		&k0sctl_phase.DetectOS{},
		lockPhase,
		&k0sctl_phase.PrepareHosts{},
		&k0sctl_phase.GatherFacts{},
		&k0sctl_phase.ValidateHosts{},
		&k0sctl_phase.GatherK0sFacts{},
		&k0sctl_phase.ValidateFacts{SkipDowngradeCheck: false},
		&k0sctl_phase.ValidateEtcdMembers{},
		&k0sctl_phase.DownloadBinaries{},
		&k0sctl_phase.UploadK0s{},
		&k0sctl_phase.DownloadK0s{},
		&k0sctl_phase.UploadFiles{},
		&k0sctl_phase.InstallBinaries{},
		&k0sctl_phase.PrepareArm{},
		&k0sctl_phase.ConfigureK0s{},
		&k0sctl_phase.InitializeK0s{},
		&k0sctl_phase.InstallControllers{},
		&k0sctl_phase.InstallWorkers{},
		&k0sctl_phase.UpgradeControllers{},
		&k0sctl_phase.UpgradeWorkers{
			NoDrain: data.NoDrain.ValueBool(),
		},
		&k0sctl_phase.ResetWorkers{
			NoDrain: data.NoDrain.ValueBool(),
		},
		&k0sctl_phase.ResetControllers{
			NoDrain: data.NoDrain.ValueBool(),
		},
		&k0sctl_phase.GetKubeconfig{},
		&k0sctl_phase.Unlock{Cancel: lockPhase.Cancel},
		&k0sctl_phase.Disconnect{},
	)

	return manager
}

func getK0sctlConfig(ctx context.Context, dia *diag.Diagnostics, data *ClusterResourceModel) *k0sctl_v1beta1.Cluster {
	k0sctlHosts := []*k0sctl_cluster.Host{}

	for _, host := range data.Hosts {
		var installFlags []string
		dia.Append(host.InstallFlags.ElementsAs(ctx, &installFlags, false)...)
		if dia.HasError() {
			return nil
		}

		var environment map[string]string
		dia.Append(host.Environment.ElementsAs(ctx, &environment, false)...)
		if dia.HasError() {
			return nil
		}

		if environment == nil {
			environment = map[string]string{}
		}

		k0sctlHosts = append(k0sctlHosts, &k0sctl_cluster.Host{
			Connection: k0s_rig.Connection{
				SSH: &k0s_rig.SSH{
					Address: host.SSH.Address.ValueString(),
					Port:    int(host.SSH.Port.ValueInt64()),
					User:    host.SSH.User.ValueString(),
					KeyPath: host.SSH.KeyPath.ValueStringPointer(),
				},
			},
			Role:             host.Role.ValueString(),
			NoTaints:         host.NoTaints.ValueBool(),
			HostnameOverride: host.Hostname.ValueString(),
			PrivateInterface: host.PrivateInterface.ValueString(),
			PrivateAddress:   host.PrivateAddress.ValueString(),
			OSIDOverride:     host.OS.ValueString(),
			InstallFlags:     installFlags,
			Environment:      environment,
		})
	}

	var config dig.Mapping
	if err := yaml.Unmarshal([]byte(data.Config.ValueString()), &config); err != nil {
		panic(err)
	}

	version, err := version.NewVersion(data.Version.ValueString())
	if err != nil {
		panic(err)
	}

	return &k0sctl_v1beta1.Cluster{
		APIVersion: k0sctl_v1beta1.APIVersion,
		Kind:       "Cluster",
		Metadata: &k0sctl_v1beta1.ClusterMetadata{
			Name: data.Name.ValueString(),
		},
		Spec: &k0sctl_cluster.Spec{
			Hosts: k0sctlHosts,
			K0s: &k0sctl_cluster.K0s{
				Version:       version,
				DynamicConfig: data.DynamicConfig.ValueBool(),
				Config:        config,
			},
		},
	}
}

func NewClusterResource() resource.Resource {
	return &ClusterResource{}
}
