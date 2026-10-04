// Package sandbox runs untrusted or generated code in isolation so the host
// machine stays safe.
//
// A [Sandbox] owns a set of language [Pack]s and one or more [Backend]s. Run
// asks the routing [Policy] which backend may execute a pack, builds an empty
// virtual world (a quota-limited in-memory filesystem, no network, capped
// output, a wall-clock deadline) and returns what the code printed and wrote.
//
// # Safety rules
//
//   - Deny by default: no network, no host files, no environment variables
//     unless the caller grants them in the [Spec].
//   - Zero never means unlimited: unset [Limits] fields take safe defaults.
//     Unlimited must be requested explicitly.
//   - No silent downgrades: if the backends allowed by the [Policy] cannot run
//     a language, Run fails with [ErrBackendUnavailable] instead of falling
//     back to weaker isolation.
//
// Backends live in sub-packages (for example backend/wasm) and are passed to
// [New] with [WithBackends]; the core package has no heavy dependencies.
package sandbox
