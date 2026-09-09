import { describe, expect, it } from "vitest";
import { hookPhases, policySettings } from "./recovery-settings";

describe("recovery settings catalog", () => {
  it("exposes 23 documented policy settings", () => {
    expect(policySettings).toHaveLength(23);
    expect(new Set(policySettings.map((item) => item.key)).size).toBe(23);
    expect(policySettings.every((item) => item.hint.trim().length > 0)).toBe(
      true,
    );
  });

  it("exposes 9 documented hook phases", () => {
    expect(hookPhases).toHaveLength(9);
    expect(new Set(hookPhases.map((item) => item[0])).size).toBe(9);
    expect(hookPhases.every((item) => item[2].trim().length > 0)).toBe(true);
  });
});
