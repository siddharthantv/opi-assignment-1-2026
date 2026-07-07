# Assignment 1 — LLM-Assisted Architecture Design for OPI DPU Operator

Submitted by: Vinay Kumar

## Contents

- `architecture_design.md` — architecture proposal, sequence diagram, CRD field
  mapping, and trade-off analysis for an Adapter-pattern integration between
  the OPI DPU Operator and NVIDIA's DPF (DOCA Platform Framework) operator.
- `llm_transcript.json` — prompts and responses used while designing the
  architecture.
- `feature_skeleton.go` — bonus Go skeleton implementing the proposed
  Reconciler; verified with `gofmt` and a successful `go run` against the
  real `k8s.io/apimachinery` and `sigs.k8s.io/controller-runtime` packages.

## Summary

OPI's operator currently reconciles Intel and Marvell DPUs but has no path
for NVIDIA hardware, while NVIDIA's DPF operator already handles BlueField-3
independently. This submission proposes a separate Adapter controller that
translates OPI's `DPUNode` CRDs into NVIDIA's DPF CRDs, letting DPF's
existing, unmodified operator do the actual hardware provisioning — maximizing
reuse rather than reimplementing NVIDIA support inside OPI.

Assumptions and known limitations are documented in `architecture_design.md`,
Section 7.
