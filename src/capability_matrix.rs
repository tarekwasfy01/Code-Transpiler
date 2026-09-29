use crate::call_resolution::EXACT_CALL_RESOLUTION_CAPABILITY;
use crate::matrix::SparseMatrix;
use crate::registry::{backend_capability, backends, CapabilityStatus};
use crate::signature::EXACT_SIGNATURE_CAPABILITY;

#[derive(Debug, Clone, PartialEq)]
pub struct CapabilityMatrix {
    pub features: Vec<String>,
    pub targets: Vec<String>,
    pub native: SparseMatrix,
    pub lowering: SparseMatrix,
    pub emulated: SparseMatrix,
    pub unsupported: SparseMatrix,
}

pub fn semantic_capability_matrix(extra: &[String]) -> CapabilityMatrix {
    let mut features = vec![
        "core",
        "integer.int32.exact",
        "integer.uint32.exact",
        "integer.int64.exact",
        "integer.uint64.exact",
        "integer.arbitrary",
        "pointer",
        "ownership.borrow",
        "exceptions",
        "classes",
        "generics",
        "concurrency",
        "ffi",
        "reflection",
        "gpu.compute",
        "index.zero_based",
        "index.negative",
        "native.go.lowering",
        "native.go.scalar",
        "native.go.functions",
        "native.c.lowering",
        "native.rust.lowering",
        "integer.int8.exact",
        "integer.uint8.exact",
        "integer.int16.exact",
        "integer.uint16.exact",
        "integer.operations.v1",
        EXACT_SIGNATURE_CAPABILITY,
        EXACT_CALL_RESOLUTION_CAPABILITY,
    ]
    .into_iter()
    .map(String::from)
    .collect::<Vec<_>>();
    for f in extra {
        if !features.contains(f) {
            features.push(f.clone());
        }
    }
    let targets = backends()
        .iter()
        .map(|b| b.id.to_string())
        .collect::<Vec<_>>();
    let n = features.len();
    let k = targets.len();
    let mut m = CapabilityMatrix {
        features,
        targets,
        native: SparseMatrix::new(n, k),
        lowering: SparseMatrix::new(n, k),
        emulated: SparseMatrix::new(n, k),
        unsupported: SparseMatrix::new(n, k),
    };
    for i in 0..n {
        for j in 0..k {
            match backend_capability(&m.features[i], &m.targets[j]).status {
                CapabilityStatus::Native => m.native.set(i, j, 1.0),
                CapabilityStatus::Lowering => m.lowering.set(i, j, 1.0),
                CapabilityStatus::Emulated => m.emulated.set(i, j, 1.0),
                CapabilityStatus::Unsupported => m.unsupported.set(i, j, 1.0),
            }
        }
    }
    m
}
impl CapabilityMatrix {
    pub fn rejected_targets(&self, requirements: &[String]) -> Result<SparseMatrix, String> {
        let mut vector = SparseMatrix::new(1, self.features.len());
        for requirement in requirements {
            let i = self
                .features
                .iter()
                .position(|f| f == requirement)
                .ok_or_else(|| {
                    format!(
                        "requirement {:?} missing from capability matrix",
                        requirement
                    )
                })?;
            vector.set(0, i, 1.0);
        }
        vector.multiply(&self.unsupported)
    }
}
