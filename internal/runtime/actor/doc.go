// MIT License
//
// Copyright (c) 2026 GoAkt Team
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
//

// Package actor provides GoAkt actors for the portcullis runtime.
//
// # Actor Topology and Spawn Model
//
// Every node runs its own set of node-local services under a GatewayManager,
// while tool execution actors are placed cluster-wide. Each actor type
// documents how it is spawned and whether it relocates in cluster mode.
//
//	Per node (children of that node's GatewayManager):
//	  GatewayManager (system.Spawn, top-level; name suffixed per node in cluster mode)
//	    ├── HealthActor (Spawn)
//	    ├── JournalActor (Spawn)
//	    ├── PolicyActor (Spawn)
//	    ├── CredentialBroker (Spawn)
//	    └── RouterActor pool (Spawn, router-0..N)
//
//	Cluster-wide (any node can host them; resolved by name via ActorOf):
//	  RegistryActor (SpawnSingleton in cluster mode, Spawn otherwise)
//	  ToolSupervisorActor (SpawnOn with round-robin placement, one per tool)
//	  sessionGrain (virtual actor, grain-engine placement, one per tenant+client+tool)
//
// # Relocation Summary
//
//   - RegistryActor: relocates in cluster mode (SpawnSingleton). The cluster
//     singleton manager restarts it on the new oldest node when its host leaves.
//   - ToolSupervisorActor: relocates. Spawned via SpawnOn (relocatable) and
//     recreated on a survivor when its host node leaves. Relocation reruns the
//     lifecycle hooks, so the circuit breaker, drain flag, and session count
//     restart from zero — the accepted v1 trade-off; the tool definition is
//     re-pulled from the Registrar so config is never stale.
//   - sessionGrain: virtual actor; re-activates on demand on a surviving node
//     the next time a router resolves its identity. The executor is rebuilt on
//     activation from the dependencies the router passes per call.
//   - Node-local services (health, journal, policy, credential broker,
//     routers): do not relocate; every node runs its own.
//
// # Cross-Node Messaging
//
// Routers resolve supervisors and session grains by deterministic name on
// every invocation, so relocation never leaves a stale reference. Messages
// that cross node boundaries are registered as serializables in
// Gateway.remoteOptions. Streaming is the one exception: a StreamingResult
// carries raw Go channels, so a session grain that receives a stream request
// from another node (detected via SessionInvokeStream.CallerNode) falls back
// to synchronous execution and returns the final result only.
//
// See the godoc on each actor type for spawn details and relocation behavior.
package actor
