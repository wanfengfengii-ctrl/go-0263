# OlivePress 橄榄鲜果冷榨线前联检闭环

## 项目目标

OlivePress 是面向橄榄油初榨合作社质检班组的单节点 Go HTTP 后端，管理一批橄榄鲜果从果园地块、采摘班次、周转箱封签、入厂批号、破碎线、惰性气体保护窗口、抽样盲码、成熟度色级板、检测孔位、异物筛查点位、理化阈值和复核人员名单锁定，到收样、盲码拆分、资源占用、成熟度计数、氧化指标核验、异物与水分复测、劣变复判、独立复核，最终形成可冷榨、已冷榨、品质隔离或已取消的唯一结论。实现采用 SQLite WAL 持久化任务聚合、盲码延迟揭示表、资源租约、覆盖格、不可覆盖证据链、仪器调用尝试、幂等操作、审计事件和终局凭据，支持重启后的确定恢复。规模规划为 24-28 个生产 Go 文件、约 2400-2700 行有效生产 Go 代码，控制在 2000-3000 行；覆盖 catalog、task、ledger、evidence、arbiter、store、api、cmd 等包；交付 go.mod、可运行 HTTP 服务和 linux/amd64、linux/arm64 双架构 Docker 构建。

## 端到端业务流程

1. 规则建档：维护虚构橄榄果园地块、品种、采摘期、破碎线、惰性气体窗口、检测孔位、色级板、异物筛查点位、人员资格和冷榨阈值规则，并为每套规则生成摘要。
2. 任务锁定：以入厂批号创建任务，冻结地块、品种、采摘班次、周转箱封签、破碎线、惰性气体窗口、盲码、色级集合、检测孔位、筛查点位、阈值和任务代次；锁定时检查采摘期、品种、摘要、人员名单与开放任务资源唯一性。
3. 收样与拆分：两名不同人员确认周转箱到厂后进入样本拆分中，盲码只写入延迟揭示表，拆分失败不得留下部分样本、揭示关系或占用记录。
4. 资源启动：为破碎线、惰性气体窗口和检测孔位建立带逻辑时间的租约；并发锁定、换线或启动由同一事务裁定，资源被占用时返回稳定错误码和排序原因。
5. 采集核验：按色级板与周转箱集合记录成熟度覆盖格，使用整数守恒规则校验青果、转色、紫黑、损伤、霉烂计数；酸价、过氧化值、多酚、水分和果温全部采用固定小数位整数运算。
6. 复判与终局：异常成熟度分布、氧化越界、异物疑点或样本分歧只创建一个当前代次复判证据；两名不同合格人员完成独立复核后，仲裁器通过单写屏障生成唯一入榨凭据、隔离结论或取消结论。

## 核心组件与职责

1. 果园品种与冷榨规则目录：catalog 包约 4 个文件，提供地块、品种、采摘期、人员资格、色级板、阈值规则、资源定义和规则摘要校验。
2. 鲜果入榨任务聚合：task 包约 5 个文件，维护状态机、任务代次、锁定快照、收样确认、幂等操作号、状态推进和终态拒绝。
3. 盲码样本与资源占用账簿：ledger 包约 4 个文件，维护盲码延迟揭示、周转箱映射、破碎线租约、惰性气体窗口租约、检测孔位租约和逻辑时钟到期规则。
4. 成熟度及理化采集账簿：evidence 包约 5 个文件，维护成熟度覆盖格、固定小数位解析、酸价/过氧化值/多酚/水分/果温读数、仪器调用尝试和不可覆盖版本链。
5. 氧化劣变复判及终局仲裁器：arbiter 包约 4 个文件，生成复判代次、隔离建议、复核记录、唯一终局凭据，并处理迟到证据隔离。
6. Go HTTP API 与持久化运行时：api、store、cmd 包约 6 个文件，提供 JSON 接口、SQLite WAL schema/migrations、事务边界、启动恢复、错误响应排序、健康检查和 Docker 入口。

## 领域规则与不变量

1. 状态只能按待锁定、待收样确认、样本拆分中、资源占用中、成熟度计数中、氧化核验中、异物复测中、待独立复核、可冷榨、已冷榨、品质隔离、已取消推进，除取消外不得跳过业务证据阶段。
2. 入厂批号与周转箱封签构成一次性收样闸门；开放任务中任何入厂批号、周转箱封签、盲码、破碎线、惰性气体窗口和检测孔位只能被一个有效任务持有。
3. 盲码样本到周转箱的映射必须延迟揭示；揭示前只能按盲码写证据，揭示必须匹配当前任务代次和已确认的样本拆分结果。
4. 相同操作号与相同请求内容必须返回原结果；相同操作号携带不同内容必须拒绝，并不得产生新的审计状态。
5. 成熟度覆盖格必须同时覆盖锁定色级集合和周转箱集合，计数总和必须等于对应周转箱样本粒数，负数、缺色级、额外色级或总量漂移均拒绝。
6. 固定小数位读数必须检查符号、长度、比例、除零和 int64 溢出；酸价、过氧化值、多酚、水分和果温的派生判定只能来自已通过解析的整数值。
7. 近红外仪、滴定仪和水分仪适配器失败只形成待重试调用记录，重试次数、目标对象和逻辑时间必须可确定，失败不得生成合格证据或释放租约。
8. 终局裁定通过任务代次上的单写屏障提交；终态后的普通操作、迟到读数、旧代次复判或额外复核必须被拒绝且不改变已持久化结论。

## 数据模型与持久化

1. catalog_plots(plot_id, cultivar_id, harvest_start, harvest_end, rule_digest)、catalog_rules(rule_id, digest, color_grades, thresholds_json, resources_json, reviewer_ids)。
2. tasks(task_id, intake_batch, generation, state, locked_snapshot_json, rule_digest, created_at_tick, final_kind, final_credential, version)。
3. crate_gates(task_id, crate_seal, confirmed_by_a, confirmed_by_b, confirmed_tick, unique open constraint on crate_seal)。
4. blind_samples(task_id, blind_code, split_index, revealed_crate_seal nullable, generation, unique open constraint on blind_code)。
5. resource_leases(task_id, resource_type, resource_id, generation, start_tick, expire_tick, released_tick nullable, unique active resource constraint)。
6. maturity_cells(task_id, generation, crate_seal, color_grade, count, version, unique cell key)。
7. evidence_versions(task_id, generation, evidence_kind, subject_key, version, fixed_value, unit_scale, raw_digest, accepted, reason_code, created_tick, immutable append-only)。
8. adapter_calls(call_id, task_id, generation, adapter_kind, target_key, attempt_no, planned_tick, outcome, payload_digest)。
9. reviews(task_id, generation, reviewer_id, role, decision, evidence_digest, created_tick, unique reviewer per generation)。
10. idempotency_keys(scope, operation_no, request_digest, response_code, response_body_json, created_tick) and audit_events(event_id, task_id, generation, actor_id, event_type, subject_key, reason_code, created_tick)。

## 公开接口

1. POST /v1/tasks/lock locks a task from catalog identifiers, crate seals, intake batch, blind codes, resources, thresholds, reviewers, rule digest and operation_no.
2. POST /v1/tasks/{id}/sample-confirm records two-person receiving confirmation for sealed crates under the current generation.
3. POST /v1/tasks/{id}/split-samples creates blind-code sample split records without revealing crate mapping.
4. POST /v1/tasks/{id}/start-resources atomically captures crusher line, inert window and test-hole leases.
5. POST /v1/tasks/{id}/maturity-counts writes full maturity coverage cells for crates and color grades.
6. POST /v1/tasks/{id}/readings submits fixed-point acid, peroxide, polyphenol, moisture and fruit-temperature readings, optionally through scripted instrument adapters.
7. POST /v1/tasks/{id}/foreign-matter submits screening findings, moisture repeat checks and affected crate/blind-code/test-hole references.
8. POST /v1/tasks/{id}/rejudge creates the current-generation deterioration recheck evidence; POST /v1/tasks/{id}/reviews records independent reviews; POST /v1/tasks/{id}/finalize competes for the single final outcome; GET /v1/tasks/{id} returns recovered state and sorted reasons.

## 失败边界

1. Every mutating endpoint runs in one SQLite transaction and records idempotency before commit; on validation failure no samples, leases, coverage cells, reveal links, evidence versions or final records are partially persisted.
2. Stable error responses use codes such as ERR_STALE_RULE_DIGEST, ERR_PLOT_CULTIVAR_WINDOW, ERR_DUPLICATE_SEAL, ERR_DUPLICATE_BLIND_CODE, ERR_RESOURCE_BUSY, ERR_OPERATION_CONFLICT, ERR_GENERATION_MISMATCH, ERR_COUNT_NOT_CONSERVED, ERR_FIXED_POINT_INVALID, ERR_FIXED_POINT_OVERFLOW, ERR_ADAPTER_RETRY_PENDING, ERR_REJUDGE_GENERATION_CONFLICT, ERR_ROLE_OVERLAP, ERR_TERMINAL_STATE.
3. Reason lists are deterministically sorted by plot_id, intake_batch, crate_seal, blind_code and test_hole before serialization, including multi-cause rejects.
4. Restart recovery rebuilds open resource occupancy, task states, pending adapter retries and final barriers from persisted rows only; no in-memory state is authoritative.
5. Adapter calls use a deterministic script and logical clock in tests; disconnect, timeout, rejection and malformed payload produce auditable retry rows without accepted evidence.
6. Late evidence for older generations remains in the append-only evidence table with accepted=false and never rewrites current-generation coverage, recheck state or final conclusion.
7. Concurrent finalization uses an optimistic version check plus unique final row; exactly one of cold-press permission, isolation or cancellation can commit.

## 验收标准

1. Locking freezes plot, cultivar, harvest shift, crate seals, intake batch, crusher line, inert window, blind codes, color board, test holes, screening points, fixed-point thresholds, reviewers and generation; stale digest or mismatched harvest window is rejected without state change.
2. Each intake batch, crate seal, blind code, crusher line, inert window and test hole is held by at most one open task; concurrent lock, line change or startup attempts produce one committed winner and deterministic rejects.
3. Receiving confirmation, sample split, reveal and review require the current generation; same operation number with identical content is idempotent, while conflicting content returns ERR_OPERATION_CONFLICT.
4. Maturity counts must cover every locked color grade and crate, use integer totals for green, turning, purple-black, damaged and moldy fruit, and reject non-conserved totals without writing a valid coverage cell.
5. Acid value, peroxide value, polyphenols, moisture and fruit temperature use fixed-scale integer parsing and arithmetic with length, sign, divide-by-zero and overflow checks; arithmetic failure writes no derived evidence.
6. Instrument rejection, disconnect, timeout or malformed data creates only deterministic retry records with target, attempt count and logical time; it never fabricates accepted evidence or releases active leases.
7. Abnormal maturity distribution, oxidation threshold breach, foreign-matter doubt or sample disagreement creates exactly one current-generation recheck evidence covering affected crates, blind codes and test holes; old-generation late readings remain immutable and do not affect the current conclusion.
8. When all required evidence is closed and two distinct qualified reviewers approve, only the successful contender creates the unique cold-press credential or alternative final conclusion; every operation after a terminal state is rejected without mutation.

## 确定性测试场景

1. catalog_task_test.go: locks a valid fictional grove intake task; rejects cultivar harvest-window mismatch; rejects stale rule digest with sorted reasons.
2. ledger_concurrency_test.go: runs synchronized goroutines competing for the same crate seal, blind code, crusher line, inert window and test hole; asserts one winner and deterministic losers after restart.
3. idempotency_generation_test.go: verifies same operation number replay, content conflict, generation mismatch on reveal/review, and role overlap rejection.
4. maturity_evidence_test.go: accepts full color-grade coverage, rejects missing grade, extra grade, negative count and non-conserved crate totals without partial cells.
5. fixedpoint_readings_test.go: covers acid/peroxide/polyphenol/moisture/temperature boundary values, scale mismatch, sign error, divide-by-zero path and overflow rejection.
6. adapter_retry_test.go: uses scripted near-infrared, titration and moisture adapters for reject, disconnect, timeout and malformed payload; asserts retry rows and no accepted substitute result.
7. rejudge_terminal_test.go: creates one deterioration recheck for affected crate/blind/test-hole set, rejects generation conflict, ignores late old-generation readings, and verifies terminal-state operation rejection.
8. final_race_recovery_test.go: races cold-press permission, isolation and cancellation through a synchronization barrier, restarts the service, and asserts the single persisted final conclusion and credential.

## 组件追踪关系

1. 果园品种与冷榨规则目录 covers harvest-window validation, cultivar matching, rule digest locking, reviewer qualification and threshold snapshots.
2. 鲜果入榨任务聚合 covers state transitions, generation checks, receiving gate, idempotency, operation conflicts and terminal-state barriers.
3. 盲码样本与资源占用账簿 covers duplicate crate seals, duplicate blind codes, delayed reveal, resource lease races, logical time and restart occupancy recovery.
4. 成熟度及理化采集账簿 covers color coverage, count conservation, fixed-point parsing, evidence immutability, adapter attempts and deterministic retry behavior.
5. 氧化劣变复判及终局仲裁器 covers threshold boundary decisions, recheck generation isolation, independent review rules, single final write and late evidence handling.
6. Go HTTP API covers stable JSON contracts, sorted reason lists, SQLite transaction boundaries, deterministic public test harness and dual-architecture container entrypoints.

## 独特性

The project centers on olive cultivar harvest timing, sealed crate intake, blind sensory-lab sampling, crusher-line readiness, inert-gas protection windows, maturity color boards, olive oxidation chemistry, foreign-material screening and cold-press admission for an olive oil cooperative. Its core behavior is a deterministic agricultural quality gate around fresh fruit condition before first pressing.
