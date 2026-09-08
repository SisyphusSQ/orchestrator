import { describe, expect, it } from "vitest";
import type { Instance } from "../api/types";
import { instanceState } from "./instance";
import { layoutInstances, moveDecision, visibleInstances } from "./topology";

const node = (
  host: string,
  parent = "",
  overrides: Partial<Instance> = {},
): Instance =>
  ({
    Key: { Hostname: host, Port: 3306 },
    MasterKey: { Hostname: parent, Port: parent ? 3306 : 0 },
    IsLastCheckValid: true,
    IsRecentlyChecked: true,
    LogBinEnabled: true,
    LogReplicationUpdatesEnabled: true,
    ReplicationSQLThreadRuning: true,
    ReplicationIOThreadRuning: true,
    ReplicationLagSeconds: { Int64: 0, Valid: true },
    ...overrides,
  }) as Instance;
describe("replication topology", () => {
  it("lays out disconnected and co-master nodes without dropping edges or looping", () => {
    const nodes = [
      node("a", "b", { IsCoMaster: true }),
      node("b", "a", { IsCoMaster: true }),
      node("c", "a"),
      node("d"),
    ];
    const layout = layoutInstances(nodes, false);
    expect(layout.size).toBe(4);
    expect(
      [...layout.values()].every(
        (p) => Number.isFinite(p.x) && Number.isFinite(p.y),
      ),
    ).toBe(true);
    expect(
      visibleInstances(nodes, new Set(["a:3306"])).map(
        (item) => item.Key.Hostname,
      ),
    ).toEqual(["a", "b", "d"]);
  });
  it("rejects cycles but preserves the explicit co-master operation", () => {
    const nodes = [node("a"), node("b", "a"), node("c", "b")];
    expect(moveDecision(nodes[0], nodes[1], nodes, "smart")).toMatchObject({
      action: "make-co-master",
      useTargetAsSource: true,
    });
    expect(moveDecision(nodes[0], nodes[2], nodes, "smart").reason).toContain(
      "环路",
    );
  });
  it("chooses move-up for grandparent and move-below for sibling in classic mode", () => {
    const nodes = [node("a"), node("b", "a"), node("c", "b"), node("d", "b")];
    expect(moveDecision(nodes[2], nodes[0], nodes, "classic").action).toBe(
      "move-up",
    );
    expect(moveDecision(nodes[2], nodes[3], nodes, "classic").action).toBe(
      "move-below",
    );
  });
  it("does not present missing replication lag or stale information as healthy", () => {
    expect(
      instanceState(
        node("b", "a", { ReplicationLagSeconds: { Int64: 0, Valid: false } }),
      ).label,
    ).toBe("延迟未知");
    expect(
      instanceState(node("b", "a", { IsRecentlyChecked: false })).label,
    ).toBe("信息过期");
  });
  it("rejects dragging a stopped replica or an unknown-lag target", () => {
    const source = node("b", "a", { ReplicationSQLThreadRuning: false });
    const target = node("c", "a");
    expect(
      moveDecision(source, target, [source, target], "smart").action,
    ).toBeUndefined();
    source.ReplicationSQLThreadRuning = true;
    target.ReplicationLagSeconds.Valid = false;
    expect(
      moveDecision(source, target, [source, target], "gtid").action,
    ).toBeUndefined();
  });
});
