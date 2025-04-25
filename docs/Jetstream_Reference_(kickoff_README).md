Perfect. I’ll compile a complete, focused guide covering:

1. A lean, reliable 80/20 CLI command set for NATS visibility and JetStream (streams, KV, object store) operations.
2. A fully self-contained Go program that embeds a JetStream-enabled server, sets up all requested structures automatically, responds to external CLI activity, and demonstrates useful advanced patterns.

This will use best practices from the official nats.io and Synadia documentation and repos. I’ll get back to you shortly with the complete code and CLI set.

## Part 1: 80/20 NATS JetStream CLI Command Set

**Streams:** Managing streams is central to JetStream. Common commands include:

- **Create a Stream:** `nats stream add <StreamName> --subjects "<subject.pattern>" [--storage file|memory] [--retention work|interest|limits]` – Define a new stream capturing given subjects. For example, `nats stream add EVENTS --subjects "events.>" --storage file` creates a file-backed stream for subjects `events.>` ([NATS by Example - Work-queue Stream (Go)](https://natsbyexample.com/examples/jetstream/workqueue-stream/go#:~:text=cfg%20%3A%3D%20jetstream.StreamConfig,)) ([JetStream Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/js_walkthrough#:~:text=nats%20pub%20foo%20,Count%7D%7D%20%40%20%7B%7B.TimeStamp)).
- **List Streams:** `nats stream ls` – List all stream names. Use `nats stream list -a` to show detailed info for each ([GitHub - nats-io/natscli: The NATS Command Line Interface](https://github.com/nats-io/natscli#:~:text=%2B,)).
- **Stream Info:** `nats stream info <StreamName>` – Show configuration and state of a stream (subjects, storage, retention, message count, etc.) ([JetStream Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/js_walkthrough#:~:text=State%3A)) ([JetStream Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/js_walkthrough#:~:text=nats%20pub%20foo%20,Count%7D%7D%20%40%20%7B%7B.TimeStamp)).
- **Read Stream Messages:** `nats stream view <StreamName>` – Continuously view messages in a stream (like tailing). Or use `nats stream get <StreamName> --seq <N>` to fetch a specific message by sequence ([JetStream Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/js_walkthrough#:~:text=nats%20pub%20foo%20,Count%7D%7D%20%40%20%7B%7B.TimeStamp)).
- **Remove/Purge Streams:** `nats stream rm <StreamName>` deletes a stream and all data. `nats stream purge <StreamName>` deletes a stream’s messages but keeps the stream (use `--subject <subj>` to purge selectively).

**Consumers:** Consumers retrieve messages from streams (via push or pull):

- **Add a Consumer:** `nats consumer add <Stream> <ConsumerName> [--filter <subject>] [--deliver <target>] [--ack explicit|none|all] [--pull] [--deliver-all] …` – Creates a consumer (durable if a name is given). For example, `nats consumer add EVENTS worker1 --filter "events.new" --ack explicit --pull` adds a durable *pull* consumer named “worker1” on stream EVENTS filtering `events.new` ([Consumers | NATS Docs](https://docs.nats.io/running-a-nats-service/nats_admin/jetstream_admin/consumers#:~:text=Again%20this%20can%20all%20be,Consumer)) ([Consumers | NATS Docs](https://docs.nats.io/running-a-nats-service/nats_admin/jetstream_admin/consumers#:~:text=%24%20nats%20con%20info%20ORDERS,DISPATCH)). For an interactive add, omit options and follow prompts ([Consumers | NATS Docs](https://docs.nats.io/running-a-nats-service/nats_admin/jetstream_admin/consumers#:~:text=nats%20con%20add)) ([Consumers | NATS Docs](https://docs.nats.io/running-a-nats-service/nats_admin/jetstream_admin/consumers#:~:text=Durable%20Name%3A%20MONITOR%20Delivery%20Subject%3A,Policy%3A%20none%20Replay%20Policy%3A%20instant)).
- **List Consumers:** `nats consumer ls <Stream>` – List consumers on a stream ([Consumers | NATS Docs](https://docs.nats.io/running-a-nats-service/nats_admin/jetstream_admin/consumers#:~:text=Listing)). For each, you’ll see the durable names.
- **Consumer Info:** `nats consumer info <Stream> <ConsumerName>` – Show details for a consumer (mode, filter, ack policy, deliver subject, pending counts, etc.) ([Consumers | NATS Docs](https://docs.nats.io/running-a-nats-service/nats_admin/jetstream_admin/consumers#:~:text=%24%20nats%20con%20info%20ORDERS,DISPATCH)) ([Consumers | NATS Docs](https://docs.nats.io/running-a-nats-service/nats_admin/jetstream_admin/consumers#:~:text=Durable%20Name%3A%20DISPATCH%20Pull%20Mode%3A,Wait%3A%2030s%20Replay%20Policy%3A%20instant)).
- **Peek/Next Message:** `nats consumer next <Stream> <ConsumerName> [--batch <N>]` – For pull consumers, retrieve the next message(s) in the queue ([NATS by Example - Work-queue Stream (Go)](https://natsbyexample.com/examples/jetstream/workqueue-stream/go#:~:text=msgs%2C%20_%20%3A%3D%20cons,msg.DoubleAck%28ctx%29)). This is useful to inspect queued messages. (For push consumers, you would `nats sub` to the deliver subject instead.)
- **Delete Consumer:** `nats consumer rm <Stream> <ConsumerName>` – Remove a consumer by name.

**Key-Value Store:** JetStream’s KV provides a simple map abstraction with history ([nats.go/jetstream/README.md at main · nats-io/nats.go · GitHub](https://github.com/nats-io/nats.go/blob/main/jetstream/README.md#:~:text=KeyValue%20Store)):

- **Create a KV Bucket:** `nats kv add <BucketName> [--history <N>] [--ttl <duration>]` – Creates a KV bucket (backed by a stream) ([Key/Value Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/key-value-store/kv_walkthrough#:~:text=For%20the%20example%20above%2C%20run,Key1)). For example, `nats kv add settings --history 5` creates a bucket with up to 5 historical revisions per key.
- **List Buckets:** `nats kv ls` – List all KV buckets (names of KV stores).
- **Put a Key:** `nats kv put <Bucket> <Key> <Value>` – Store a value. For example, `nats kv put settings logLevel debug` updates `logLevel` ([Key/Value Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/key-value-store/kv_walkthrough#:~:text=If%20we%20now%20concurrently%20change,kv%27%20by)).
- **Get a Value:** `nats kv get <Bucket> <Key>` – Retrieve the latest value for a key.
- **Watch for Changes:** `nats kv watch <Bucket> [<KeyPattern>]` – Subscribe to updates in real-time. For example, `nats kv watch my-kv` will print every update or deletion in the bucket ([Key/Value Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/key-value-store/kv_walkthrough#:~:text=For%20the%20example%20above%2C%20run,Key1)) ([Key/Value Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/key-value-store/kv_walkthrough#:~:text=%5B2021,Key1)). This leverages the KV’s ability to retain history and send deltas.
- **Key History:** `nats kv history <Bucket> <Key>` – Show historical values/revisions for a key (if history > 1).
- **Delete a Key/Bucket:** `nats kv del <Bucket> <Key>` removes a key (tombstones it). `nats kv rm <Bucket>` deletes the entire bucket ([Key/Value Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/key-value-store/kv_walkthrough#:~:text=Cleaning%20up)).

**Object Store:** For large binary data, JetStream object store commands are used ([Object Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/obj_store/obj_walkthrough#:~:text=Copy)):

- **Create an Object Bucket:** `nats object add <BucketName>` – Initialize a new object store bucket. e.g., `nats object add files` ([Object Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/obj_store/obj_walkthrough#:~:text=Copy)).
- **List Buckets / Objects:** `nats object ls` lists buckets, and `nats object ls <Bucket>` lists objects in a bucket with size and date ([Object Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/obj_store/obj_walkthrough#:~:text=%E2%94%9C%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%AC%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%AC%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%80%E2%94%A4%20%E2%94%82%20Name%20%20,%E2%94%82%20Time%20%E2%94%82)).
- **Put an Object:** `nats object put <Bucket> <filePath> [--name <objectName>]` – Upload a file as an object. By default the object name is the file path; you can override with `--name`. For example, `nats object put images cat.png` uploads *cat.png* to the *images* bucket ([Object Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/obj_store/obj_walkthrough#:~:text=Putting%20a%20file%20in%20the,bucket)).
- **Get an Object:** `nats object get <Bucket> <ObjectName> [--output <filePath>]` – Download an object’s contents. For example, `nats object get images cat.png --output ~/Downloads/cat.png` will retrieve the file ([Object Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/obj_store/obj_walkthrough#:~:text=Getting%20an%20object%20from%20the,bucket)) ([Object Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/obj_store/obj_walkthrough#:~:text=nats%20object%20get%20myobjbucket%20,logo.mov)).
- **Remove an Object:** `nats object rm <Bucket> <ObjectName>` – Delete an object from the bucket ([Object Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/obj_store/obj_walkthrough#:~:text=Removing%20an%20object%20from%20the,bucket)).
- **Object Info:** `nats object info <Bucket> <ObjectName>` shows metadata (size, chunks, digest) for an object. `nats object info <Bucket>` shows bucket status ([Object Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/obj_store/obj_walkthrough#:~:text=Copy)) ([Object Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/obj_store/obj_walkthrough#:~:text=TTL%3A%20unlimitd%20Sealed%3A%20false%20Size%3A,Kind%3A%20JetStream%20JetStream%20Stream%3A%20OBJ_myobjbucket)).
- **Watch Bucket:** `nats object watch <Bucket>` – Stream updates (puts and deletes) to objects in real-time ([Object Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/obj_store/obj_walkthrough#:~:text=Watching%20for%20changes%20to%20a,bucket)).

**JetStream Observability & Admin:** Monitoring commands help inspect system status ([GitHub - nats-io/natscli: The NATS Command Line Interface](https://github.com/nats-io/natscli#:~:text=The%20,a%20human%20friendly%20textual%20output)):

- **Account Info:** `nats account info` – Shows JetStream usage and limits for the current account. This displays memory/storage used, number of streams & consumers, etc. (If JetStream is not enabled, it will say “not supported”) ([Object Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/obj_store/obj_walkthrough#:~:text=JetStream%20Account%20Information%3A)).
- **Stream/Consumer Health Check:** `nats server check stream` and `nats server check consumer` – Run health checks on all streams or consumers (e.g., report if any consumer is lagging or stream lost replicas) ([GitHub - nats-io/natscli: The NATS Command Line Interface](https://github.com/nats-io/natscli#:~:text=The%20,health%20of%20Streams%20and%20Consumers)). These commands output status in a Nagios/Prometheus-friendly format or human-readable summary.
- **Server Stats:** `nats server info` – General server information, and `nats server list` or `nats server report` – Overviews of connected servers and their JetStream stats (memory, streams, etc.) ([GitHub - nats-io/natscli: The NATS Command Line Interface](https://github.com/nats-io/natscli#:~:text=%2B,)) ([GitHub - nats-io/natscli: The NATS Command Line Interface](https://github.com/nats-io/natscli#:~:text=Additional%20to%20this%20various%20reports,the%20results%20by%20account%20etc)).
- **Publish/Subscribe (for testing):** Use core NATS commands to test the streams. For example, `nats pub <subject> <message>` to produce messages into JetStream (if the subject is backed by a stream, it will be stored) ([JetStream Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/js_walkthrough#:~:text=nats%20pub%20foo%20,Count%7D%7D%20%40%20%7B%7B.TimeStamp)). Use `nats sub <subject>` or `nats sub --js <subject>` to consume (the latter flag attaches a JetStream consumer for durable delivery). These help in dynamically verifying stream behavior (e.g., publishing events and watching consumers receive them).

Using the above CLI commands covers the **80/20** of JetStream daily operations – creating streams/consumers, publishing/reading messages, and inspecting the system state – allowing you to administer and test a running JetStream deployment effectively.

## Part 2: Self-Contained Embedded JetStream Example (Go)

Below is a Go program that embeds a NATS server **with JetStream enabled** in-process and dynamically manages streams, consumers, a Key-Value store, and more. It uses the latest official **nats.go** client and **nats-server** libraries to demonstrate advanced JetStream patterns as of NATS 2.11:

### Embedded JetStream Server with Dynamic Consumers Example

<important-note for llm coding agent>
The single file runnable is currently found at internal/core.go

we need to slightly adopt it to fit with our project structure of bin/ cmd/ internal/ store/

</important-note>

**How it works:** On startup, the program creates two JetStream streams: **COMMAND** (for subjects `command.>` with WorkQueue retention) and **EVENT** (for `event.>`). It also creates a JetStream **Key-Value bucket** named “sessions”. This KV store holds JSON records of active sessions, where each key is a session ID and the value lists that session’s subscribed subjects. A watcher is placed on the “sessions” bucket to react to any changes ([nats.go/jetstream/README.md at main · nats-io/nats.go · GitHub](https://github.com/nats-io/nats.go/blob/main/jetstream/README.md#:~:text=Watching%20for%20changes%20on%20a,bucket)) ([Key/Value Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/key-value-store/kv_walkthrough#:~:text=For%20the%20example%20above%2C%20run,Key1)).

When a session’s subscription list is added or updated (via a KV `PUT`), the program computes the difference (new subscriptions or removed subscriptions) using the KV entry’s history ([nats.go/jetstream/README.md at main · nats-io/nats.go · GitHub](https://github.com/nats-io/nats.go/blob/main/jetstream/README.md#:~:text=Watcher%20supports%20several%20configuration%20options%3A)) ([nats.go/jetstream/README.md at main · nats-io/nats.go · GitHub](https://github.com/nats-io/nats.go/blob/main/jetstream/README.md#:~:text=%2F%2F%20After%20that%2C%20watcher%20will,sue.age)). For each **new subject**, the program ensures there is an ephemeral **push consumer** on the appropriate stream (COMMAND or EVENT) filtered to that subject, creating one if necessary. It then creates a NATS subscriber (representing that session) on the consumer’s delivery subject so the session will receive messages for that subject. All sessions that share the same subject reuse one consumer (the message is delivered once into JetStream and then fan-out to all session subscribers), ensuring efficient delivery. For each **removed subject** (or deleted session), the program unsubscribes the session’s subscriber and, if no other sessions are interested in that subject, it deletes the ephemeral consumer to free resources. This **coordinated ephemeral subscriber** pattern means the system automatically adapts to interest changes without manual intervention.

The program also sets up a durable **work-queue consumer** on `command.x`. This is created via a queue subscription with a durable name “COMMAND_X” on the COMMAND stream (filtering subject `command.x`). Multiple workers could share the load by using the same queue group. Here we use one worker in-process with a callback handler that processes the message and calls `Ack()` when done, ensuring the message is only processed once ([NATS by Example - Work-queue Stream (Go)](https://natsbyexample.com/examples/jetstream/workqueue-stream/go#:~:text=msgs%2C%20_%20%3A%3D%20cons,msg.DoubleAck%28ctx%29)) ([NATS by Example - Work-queue Stream (Go)](https://natsbyexample.com/examples/jetstream/workqueue-stream/go#:~:text=for%20msg%20%3A%3D%20range%20msgs,s%5Cn%22%2C%20msg.Subject%28%29%29%20msg.Ack%28%29)). The COMMAND stream uses WorkQueue retention, so once a `command.x` message is acknowledged by this consumer, it is removed from the stream ([JetStream Model Deep Dive | NATS Docs](https://docs.nats.io/using-nats/developer/develop_jetstream/model_deep_dive#:~:text=)).

To audit or monitor the processed commands, we create a **mirrored stream** named `COMMANDX_MIRROR` that mirrors the COMMAND stream but filtered to `command.x` messages ([Source and Mirror Streams | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/source_and_mirror#:~:text=%2A%20%60FilterSubject%60%20,SubjectTransforms)). This stream will receive a copy of every `command.x` message as it’s published (regardless of whether it was consumed/acknowledged in the work queue). This provides a log of all tasks that went through the work queue.

Finally, we demonstrate an advanced **subject transform** pattern: the program creates a stream `EVENT_MIGRATED` that sources from the EVENT stream with a subject transform that renames the prefix `event.` to `evt.` ([Subject Mapping and Partitioning | NATS Docs](https://docs.nats.io/nats-concepts/subject_mapping#:~:text=Mapping%20a%20full%20wildcard%20%27)) ([Subject Mapping and Partitioning | NATS Docs](https://docs.nats.io/nats-concepts/subject_mapping#:~:text=Wildcard%20tokens%20may%20be%20referenced,the%20second%20wildcard%20token%2C%20etc)). This means any message on `event.foo.bar` in the source will appear as `evt.foo.bar` in the new stream. This could be used for migrating subjects or replicating events under a new namespace (e.g., for a new version of a service) without changing publishers. (This uses JetStream’s subject transformation feature introduced in NATS 2.10+.)

### Example Usage

You can run this program (e.g., `go run main.go`). Once running, use **NATS CLI** commands to interact with it:

- **Register a session and subscribe to subjects:** Use the KV CLI to add session entries. For example:  
  ```bash
  nats kv put sessions Alice '{"subscriptions":["event.orders.created","event.inventory.updated"]}'
  ```  
  This declares a session “Alice” interested in two subjects. The program will detect this and automatically create consumers for `event.orders.created` and `event.inventory.updated` (if not already present) and subscribe Alice to them. You should see log output like:  
  *“Created ephemeral consumer for subject ‘event.orders.created’… Session "Alice" subscribed to subject "event.orders.created"”*.  
  The KV watcher sends the current value on start, so if you put multiple sessions before the program starts, it will process them on launch ([nats.go/jetstream/README.md at main · nats-io/nats.go · GitHub](https://github.com/nats-io/nats.go/blob/main/jetstream/README.md#:~:text=%2F%2F%20First%2C%20the%20watcher%20sends,%22blue)).

- **Publish events:** Now publish messages to those subjects. For example:  
  ```bash
  nats pub event.orders.created '{"order_id":123, "status":"NEW"}'
  nats pub event.inventory.updated '{"product":"Widget", "qty":100}'
  ```  
  The embedded system will receive these messages (via the JetStream consumers) and log deliveries to each session. For instance, the log might show:  
  *“📥 Session "Alice" received message on [event.orders.created]: '{"order_id":123, "status":"NEW"}'”*.  
  If you add another session with interest in the same subjects (e.g., session “Bob”), the message fan-out will deliver to both Alice and Bob via the single ephemeral consumer per subject.

- **Dynamic subscription changes:** You can update a session’s subscriptions and the system will adjust. For example:  
  ```bash
  nats kv put sessions Alice '{"subscriptions":["event.orders.created"]}'
  ```  
  Removing the second subject from Alice’s list will cause the program to unsubscribe Alice from `event.inventory.updated` and if no other session cares about that subject, it will delete the ephemeral consumer for it (logged as *“removed ephemeral consumer for subject … (no active sessions)”*). Similarly, deleting a session (`nats kv del sessions Alice`) triggers cleanup of all its subscriptions.

- **Work-Queue command processing:** Publish some command messages for the work-queue consumer to handle:  
  ```bash
  nats pub command.x "Run backup job"
  nats pub command.x "Recompute metrics"
  ```  
  These messages go into the COMMAND stream and are delivered to the durable consumer “COMMAND_X”. The program’s handler will log processing (e.g., *“⚙️  Processing command.x message: "Run backup job"”*) and ack them. Because of WorkQueue retention, the COMMAND stream will not retain them after ack. However, the mirrored stream `COMMANDX_MIRROR` will have a copy. You can verify by checking:  
  ```bash
  nats stream info COMMANDX_MIRROR
  ```  
  It should show the total messages equal to the number of `command.x` publishes made (even if COMMAND shows zero). You could also do `nats stream view COMMANDX_MIRROR` to see the content of those command messages stored.

- **Inspect consumers and streams:** Use CLI to see how the system created consumers on the fly. For example:  
  ```bash
  nats consumer ls EVENT
  ```  
  This will list ephemeral consumers on the EVENT stream – you’ll see entries with auto-generated names corresponding to subjects like `event.orders.created`, etc. (Since they are ephemeral, their durable name is the server-generated name) ([NATS by Example - Work-queue Stream (Go)](https://natsbyexample.com/examples/jetstream/workqueue-stream/go#:~:text=Exclusive%20non)) ([NATS by Example - Work-queue Stream (Go)](https://natsbyexample.com/examples/jetstream/workqueue-stream/go#:~:text=Multiple%20filtered%20consumers)). You can also run `nats stream info EVENT --consumer` to get stream info including consumers. Similarly, `nats consumer ls COMMAND` will show the `COMMAND_X` durable and any ephemeral for `command.*` subjects if present.  
  Check the Key-Value bucket state with `nats kv get sessions Alice` (it should return the JSON you set), or list all sessions with `nats kv keys sessions`.

- **Transformed stream demonstration:** The `EVENT_MIGRATED` stream is sourcing from EVENT with a transform (prefix `evt.`). If you publish an event (as above), you can observe it also appearing under the new subject. For example, run:  
  ```bash
  nats sub 'evt.>' &
  nats pub event.orders.created 'test event'
  ```  
  The subscription on `evt.>` should receive the message as `evt.orders.created` – confirming the transform mapping from `event.orders.created` ([Subject Mapping and Partitioning | NATS Docs](https://docs.nats.io/nats-concepts/subject_mapping#:~:text=Mapping%20a%20full%20wildcard%20%27)). The `EVENT_MIGRATED` stream will store these under the new prefix.

This embedded setup illustrates a dynamic JetStream application: as you use CLI commands to publish or update the KV store, the Go program **reconfigures itself** in real-time. New subjects are automatically handled by ephemeral consumers, sessions immediately start receiving relevant events, and work-queue tasks are processed and recorded. All of this is accomplished using official NATS APIs and demonstrates patterns like **auto-fanout with ephemeral consumers**, **durable queued processing**, **stream mirroring for audit**, and **subject transformations for migration**, as referenced in NATS documentation. By combining these building blocks, you get a powerful, self-adjusting messaging system that responds to your CLI inputs and data flows. 

**Sources:** This solution is based on NATS JetStream official docs and examples, including the NATS CLI reference ([Consumers | NATS Docs](https://docs.nats.io/running-a-nats-service/nats_admin/jetstream_admin/consumers#:~:text=Creating%20Push)) ([Object Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/obj_store/obj_walkthrough#:~:text=Copy)), JetStream key-value and object store guides ([Key/Value Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/key-value-store/kv_walkthrough#:~:text=For%20the%20example%20above%2C%20run,Key1)) ([Object Store Walkthrough | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/obj_store/obj_walkthrough#:~:text=Getting%20an%20object%20from%20the,bucket)), and JetStream architectural features like work-queue retention ([JetStream Model Deep Dive | NATS Docs](https://docs.nats.io/using-nats/developer/develop_jetstream/model_deep_dive#:~:text=)), mirrors/filters ([Source and Mirror Streams | NATS Docs](https://docs.nats.io/nats-concepts/jetstream/source_and_mirror#:~:text=%2A%20%60FilterSubject%60%20,SubjectTransforms)), and subject mapping ([Subject Mapping and Partitioning | NATS Docs](https://docs.nats.io/nats-concepts/subject_mapping#:~:text=Mapping%20a%20full%20wildcard%20%27)). The patterns used (ephemeral consumer per subject, durable pull/queue consumer, stream sourcing with transforms) are drawn from the JetStream model and capabilities described in the NATS docs. This end-to-end example should compile and run with the latest NATS Server **2.11** and Go client libraries, illustrating JetStream usage in a real-world scenario.