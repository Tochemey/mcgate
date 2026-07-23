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
// # Actor Hierarchy and Spawn Model
//
// The runtime actor tree is rooted at GatewayManager, spawned by Gateway.Start.
// Each actor type documents how it is spawned and whether it relocates in cluster mode.
//
//	Gateway (process handle, not an actor)
//	  └── GatewayManager (system.Spawn, top-level)
//	        ├── RegistryActor (Spawn or SpawnSingleton when cluster)
//	        │     └── ToolSupervisorActor (SpawnChild, one per tool)
//	        │           └── SessionActor (SpawnChild, one per tenant+client+tool)
//	        ├── HealthActor (Spawn)
//	        ├── JournalActor (Spawn)
//	        ├── PolicyActor (Spawn)
//	        ├── CredentialBroker (Spawn, when providers configured)
//	        └── RouterActor (Spawn)
//
// # Relocation Summary
//
//   - RegistryActor: Relocates in cluster mode (SpawnSingleton). The cluster
//     singleton manager may move it to another node on failure or rebalancing.
//   - All other actors: Do not relocate. They run on the node where their parent
//     runs. ToolSupervisorActor and SessionActor follow RegistryActor's node in
//     cluster mode.
//
// See the godoc on each actor type for spawn details and relocation behavior.
package actor
