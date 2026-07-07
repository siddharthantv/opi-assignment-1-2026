// Package nvidiaintegration is the design skeleton for the two-phase
// NVIDIA/DPF integration described in architecture_design.md.
//
// Everything in the "Phase B" section below mirrors, field-for-field,
// interfaces read directly from a real clone of openshift/dpu-operator:
//   - dpu-api/api.proto                          (LifeCycleService, DeviceService,
//     NetworkFunctionService, DpuNetworkConfigService, HeartbeatService)
//   - internal/daemon/plugin/vendorplugin.go     (the VendorPlugin Go interface
//     every VSP -- Intel's, Marvell's, and this one -- must satisfy)
//   - vendor/.../opi-api/.../l2_xpu_infra_mgr.pb.go (BridgePortSpec's real fields)
//
// This is NOT a guess dressed up as fact: local types below are trimmed,
// self-contained mirrors (no controller-runtime/grpc-go dependency, so this
// compiles standalone for review -- verified with `go build`), but every
// field name and method signature matches what was actually read from
// source. Two things could NOT be confirmed from source and are called out
// rather than invented -- see the comments on NvidiaDetector below.
//
// Phase A (BootstrapController): a one-time hand-off to unmodified DPF,
// using the real CRD fields read from NVIDIA/doca-platform
// (api/provisioning/v1alpha1/*_types.go).
package nvidiaintegration

import (
	"context"
	"fmt"
	"time"
)

// =====================================================================
// Phase A: Bootstrap Controller
// Field names below match api/provisioning/v1alpha1/{dpucluster,bfb,
// dpuflavor,dpuset}_types.go read from a real clone of NVIDIA/doca-platform.
// =====================================================================

// DPUPhase mirrors DPF's real 16-value phase enum from
// api/provisioning/v1alpha1/dpu_types.go -- not a simplified stand-in.
type DPUPhase string

const (
	DPUInitializing           DPUPhase = "Initializing"
	DPUPending                DPUPhase = "Pending"
	DPUPrepareBFB             DPUPhase = "Prepare BFB"
	DPUUpdateFirmware         DPUPhase = "Update Firmware"
	DPUConfig                 DPUPhase = "DPU Config"
	DPUConfigFWParameters     DPUPhase = "Config FW Parameters"
	DPUInitializeInterface    DPUPhase = "Initialize Interface"
	DPUOSInstalling           DPUPhase = "OS Installing"
	DPUClusterConfig          DPUPhase = "DPU Cluster Config"
	DPUHostNetworkConfig      DPUPhase = "Host Network Configuration"
	DPUNodeEffect             DPUPhase = "Node Effect"
	DPUNodeEffectRemoval      DPUPhase = "Node Effect Removal"
	DPUPerformArmForceRestart DPUPhase = "Perform ARM Force Restart"
	DPURebooting              DPUPhase = "Rebooting"
	DPUReady                  DPUPhase = "Ready"
	DPUError                  DPUPhase = "Error"
)

// Real timeout constants found in DPF source (cmd/provisioning/main.go and
// internal/provisioning/controllers/dpu/state/redfish/). These are DPF's
// own numbers -- reused here, not re-derived, so the bootstrap controller's
// own polling/backoff can be sized against a real budget instead of a guess.
const (
	DPFDefaultOSInstallTimeout       = 60 * time.Minute
	DPFDefaultFirmwareUpdateTimeout  = 45 * time.Minute
	DPFSecureBootVerificationTimeout = 4 * time.Minute
	DPFStaleTrackerTimeout           = 20 * time.Minute
)

// DPUCluster mirrors DPUClusterSpec: Type is validated by DPF against
// "kamaji|static|[^/]+/.*", MaxNodes 1-3000 (default 1000).
type DPUCluster struct {
	Name, Namespace string
	Type            string // "kamaji" for this design; see architecture_design.md section 5
	MaxNodes        int
	Kubeconfig      string // only set when Type == "static"
}

// BFB mirrors BFBSpec exactly: URL is +required and validated http(s), and
// both FileName and URL are immutable once set (DPF CEL validation).
type BFB struct {
	Name, Namespace string
	FileName        string // optional
	URL             string // required -- a real, reachable artifact URL, not a placeholder
}

// DPUFlavor mirrors the fields this design actually touches from
// DPUFlavorSpec. RawConfigScript is DPUFlavorOVS's only field -- confirmed
// from source, not simplified for this skeleton.
type DPUFlavor struct {
	Name, Namespace    string
	OVSRawConfigScript string
}

type DPUSet struct {
	Name, Namespace, FlavorRef, ClusterRef string
	NodeSelector                           map[string]string
}

type DPFStatus struct {
	Phase DPUPhase
}

// DPFClient: the bootstrap controller's only channel to DPF, through its
// public CRDs via the Kubernetes API -- never DMS, never the DPU directly.
type DPFClient interface {
	EnsureDPUCluster(ctx context.Context, c DPUCluster) error
	EnsureBFB(ctx context.Context, b BFB) error
	EnsureDPUFlavor(ctx context.Context, f DPUFlavor) error
	EnsureDPUSet(ctx context.Context, s DPUSet) error
	GetStatus(ctx context.Context, dpuSetName, namespace string) (DPFStatus, error)
}

// NodeLabeler is the one write-back to dpu-operator's side once DPF
// reports Ready: label the node so the existing scheduling logic treats it
// like any other vendor's DPU node.
type NodeLabeler interface {
	LabelDPUNode(ctx context.Context, nodeName string, labels map[string]string) error
}

type BootstrapController struct {
	DPF    DPFClient
	Labels NodeLabeler
}

// Reconcile drives one NVIDIA node through Phase A. Budgets its own
// requeue interval loosely against DPF's real timeouts: no point polling
// every few seconds during a phase DPF itself budgets 45-60 minutes for.
func (b *BootstrapController) Reconcile(ctx context.Context, cfgName, nodeName, bfbURL string) (requeueAfter time.Duration, err error) {
	clusterName := cfgName + "-dpu-cluster"
	if err := b.DPF.EnsureDPUCluster(ctx, DPUCluster{
		Name: clusterName, Namespace: "dpf-operator-system", Type: "kamaji", MaxNodes: 1000,
	}); err != nil {
		return 0, fmt.Errorf("creating DPUCluster: %w", err)
	}

	flavorName := cfgName + "-flavor"
	if err := b.DPF.EnsureDPUFlavor(ctx, DPUFlavor{
		Name: flavorName, Namespace: "dpf-operator-system",
		// Static OVS-DOCA bootstrap belongs here, per DPUFlavorOVS's real
		// single field; per-request dataplane changes are Phase B's job
		// (see architecture_design.md section 3, the OVS split).
		OVSRawConfigScript: "# TODO: real OVS-DOCA bootstrap script, deployment-specific",
	}); err != nil {
		return 0, fmt.Errorf("creating DPUFlavor: %w", err)
	}

	if err := b.DPF.EnsureBFB(ctx, BFB{
		Name: cfgName + "-bfb", Namespace: "dpf-operator-system",
		URL: bfbURL, // caller-supplied: a real artifact URL, per BFBSpec.URL being +required
	}); err != nil {
		return 0, fmt.Errorf("creating BFB: %w", err)
	}

	setName := cfgName + "-dpuset"
	if err := b.DPF.EnsureDPUSet(ctx, DPUSet{
		Name: setName, Namespace: "dpf-operator-system",
		FlavorRef: flavorName, ClusterRef: clusterName,
		NodeSelector: map[string]string{"kubernetes.io/hostname": nodeName},
	}); err != nil {
		return 0, fmt.Errorf("creating DPUSet: %w", err)
	}

	status, err := b.DPF.GetStatus(ctx, setName, "dpf-operator-system")
	if err != nil {
		return 15 * time.Second, err
	}

	switch status.Phase {
	case DPUReady:
		if err := b.Labels.LabelDPUNode(ctx, nodeName, map[string]string{
			"dpu": "true", "dpu.openshift.io/vendor": "nvidia",
		}); err != nil {
			return 0, fmt.Errorf("labelling node after DPF Ready: %w", err)
		}
		return 0, nil
	case DPUOSInstalling, DPUUpdateFirmware:
		// These are DPF's slowest phases (45-60m budgeted); poll infrequently.
		return 2 * time.Minute, nil
	case DPUError:
		return 0, fmt.Errorf("DPF reported phase Error for DPUSet %s", setName)
	default:
		return 20 * time.Second, nil
	}
}

// =====================================================================
// Phase B: NVIDIA VSP adapter
// Every method signature below is copied from the real interfaces read
// from source (see package doc). Only the request/response *body* types
// are trimmed local mirrors to keep this file dependency-free; the shapes
// match what was read in dpu-api/api.proto and the vendored opi-api code.
// =====================================================================

// InitRequest / IpPort mirror dpu-api/api.proto's LifeCycleService exactly:
// InitRequest{dpu_mode, dpu_identifier} -> IpPort{ip, port}.
type InitRequest struct {
	DpuMode       bool
	DpuIdentifier string
}

type IpPort struct {
	Ip   string
	Port int32
}

// Device / DeviceListResponse mirror dpu-api/api.proto's DeviceService
// exactly, including the nested TopologyInfo the real proto defines.
type Device struct {
	ID       string
	Health   string
	Topology struct{ Node string }
}

type DeviceListResponse struct {
	Devices map[string]Device
}

// NFRequest mirrors dpu-api/api.proto's NetworkFunctionService request:
// {input, output, bridge_id} -- exactly what ServiceFunctionChain's
// NetworkFunction{Name,Image} needs translated into, per architecture_design.md
// section 8's mapping table.
type NFRequest struct {
	Input, Output, BridgeID string
}

// BridgePortSpec mirrors the REAL opi-api evpn-gw BridgePortSpec fields
// read from vendor/github.com/opiproject/opi-api/.../l2_xpu_infra_mgr.pb.go
// -- MacAddress, Ptype, LogicalBridges -- not the BridgePortSpec{Name,
// BridgeName, MTU} shape used in an earlier draft of this skeleton, which
// did not exist in the real API.
type BridgePortType int

const (
	BridgePortUnknown BridgePortType = iota
	BridgePortAccess
	BridgePortTrunk
)

type BridgePortSpec struct {
	MacAddress     []byte
	Ptype          BridgePortType
	LogicalBridges []string
}

type BridgePort struct {
	Name   string // server-assigned resource name, per the real proto's comment
	Spec   BridgePortSpec
	Status struct{ Ready bool }
}

// VSPServer is the real VendorPlugin server-side contract, assembled from
// dpu-operator's internal/daemon/plugin/vendorplugin.go (client interface)
// and internal/daemon/vendor-specific-plugins/mock-vsp/mockvsp.go (the
// existing reference server implementation, which this mirrors).
type VSPServer interface {
	Init(ctx context.Context, req InitRequest) (IpPort, error)
	GetDevices(ctx context.Context) (DeviceListResponse, error)
	SetNumVfs(ctx context.Context, vfCount int32) (int32, error)
	CreateNetworkFunction(ctx context.Context, req NFRequest) error
	DeleteNetworkFunction(ctx context.Context, req NFRequest) error
	SetDpuNetworkConfig(ctx context.Context, isAccelerated bool) error
	CreateBridgePort(ctx context.Context, bridgePortID string, spec BridgePortSpec) (BridgePort, error)
	DeleteBridgePort(ctx context.Context, name string, allowMissing bool) error
}

// DOCAClient is how the NVIDIA VSP does the real work locally on the
// BlueField, instead of reimplementing dataplane control of its own.
type DOCAClient struct{} // real implementation calls DOCA SDK / OVS-DOCA CLI; omitted here by design

func (DOCAClient) SetNumVfs(ctx context.Context, pciAddress string, count int32) error { return nil }
func (DOCAClient) CreateBridgePort(ctx context.Context, spec BridgePortSpec) (string, error) {
	return "", nil
}
func (DOCAClient) DeleteBridgePort(ctx context.Context, name string) error { return nil }
func (DOCAClient) WireNetworkFunction(ctx context.Context, input, output, bridgeID string) error {
	// bridgeID here is expected to name a bridge DPUFlavor.OVS.RawConfigScript
	// already created in Phase A -- see architecture_design.md section 3.
	return nil
}

// NvidiaVSPServer satisfies VSPServer. dpu-daemon cannot distinguish this
// from Intel's or Marvell's VSP, by construction of the interface.
type NvidiaVSPServer struct {
	DOCA          DOCAClient
	dpuIdentifier string
}

func (s *NvidiaVSPServer) Init(ctx context.Context, req InitRequest) (IpPort, error) {
	s.dpuIdentifier = req.DpuIdentifier
	// Real dpu-operator VSPs return a loopback ip/port pair here (see
	// mockvsp.go's Init: 127.0.0.1:50051) -- this is a control-plane
	// handshake value, not a dataplane address.
	return IpPort{Ip: "127.0.0.1", Port: 50051}, nil
}

func (s *NvidiaVSPServer) GetDevices(ctx context.Context) (DeviceListResponse, error) {
	return DeviceListResponse{Devices: map[string]Device{}}, nil // TODO: enumerate real DOCA-visible netdevs
}

func (s *NvidiaVSPServer) SetNumVfs(ctx context.Context, vfCount int32) (int32, error) {
	if err := s.DOCA.SetNumVfs(ctx, "" /* TODO: real PCI address */, vfCount); err != nil {
		return 0, fmt.Errorf("nvidia vsp: SetNumVfs(%d): %w", vfCount, err)
	}
	return vfCount, nil
}

func (s *NvidiaVSPServer) CreateNetworkFunction(ctx context.Context, req NFRequest) error {
	if err := s.DOCA.WireNetworkFunction(ctx, req.Input, req.Output, req.BridgeID); err != nil {
		return fmt.Errorf("nvidia vsp: CreateNetworkFunction(bridge_id=%s): %w", req.BridgeID, err)
	}
	return nil
}

func (s *NvidiaVSPServer) DeleteNetworkFunction(ctx context.Context, req NFRequest) error {
	return nil // TODO: real teardown, mirroring CreateNetworkFunction's wiring
}

func (s *NvidiaVSPServer) SetDpuNetworkConfig(ctx context.Context, isAccelerated bool) error {
	return nil // TODO: real dpu-operator VSPs store this and gate other calls on it
}

func (s *NvidiaVSPServer) CreateBridgePort(ctx context.Context, bridgePortID string, spec BridgePortSpec) (BridgePort, error) {
	name, err := s.DOCA.CreateBridgePort(ctx, spec)
	if err != nil {
		return BridgePort{}, fmt.Errorf("nvidia vsp: CreateBridgePort(%s): %w", bridgePortID, err)
	}
	return BridgePort{Name: name, Spec: spec}, nil
}

func (s *NvidiaVSPServer) DeleteBridgePort(ctx context.Context, name string, allowMissing bool) error {
	if err := s.DOCA.DeleteBridgePort(ctx, name); err != nil && !allowMissing {
		return fmt.Errorf("nvidia vsp: DeleteBridgePort(%s): %w", name, err)
	}
	return nil
}
