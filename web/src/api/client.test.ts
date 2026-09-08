import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, endpoint, execute, inferPrefix, query } from "./client";

afterEach(() => vi.unstubAllGlobals());
describe("HTTP contract", () => {
  it("encodes each path parameter once and detects deployment prefix", () => {
    expect(endpoint("search", "a/b+c%d")).toBe("/search/a%2Fb%2Bc%25d");
    expect(inferPrefix("/orch/web/cluster/a")).toBe("/orch");
    expect(inferPrefix("/website")).toBe("");
  });
  it("accepts raw lists and successful envelopes", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(new Response("[1,2]"))
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ Code: "OK", Details: "enabled" })),
      );
    vi.stubGlobal("fetch", fetch);
    expect(await query("/clusters")).toEqual([1, 2]);
    expect(await query("/check-global-recoveries")).toBe("enabled");
  });
  it("rejects HTTP 200 business errors and retains partial results", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            Code: "ERROR",
            Message: "partially relocated",
            Details: { moved: 1 },
          }),
        ),
      ),
    );
    await expect(execute("/relocate/a/1/b/2")).rejects.toMatchObject({
      message: "partially relocated",
      details: { moved: 1 },
    });
  });
  it.each(["network", "invalid-json", "missing-code", "indeterminate"])(
    "does not replay an uncertain %s action",
    async (failure) => {
      const fetch = vi.fn();
      if (failure === "network")
        fetch.mockRejectedValue(new TypeError("network error"));
      else
        fetch.mockResolvedValue(
          new Response(
            failure === "invalid-json"
              ? "broken"
              : failure === "missing-code"
                ? "{}"
                : JSON.stringify({
                    Code: "ERROR",
                    ErrorClass: "indeterminate",
                    Message: "lost acknowledgement",
                  }),
          ),
        );
      vi.stubGlobal("fetch", fetch);
      await expect(execute("/stop-replica/a/3306")).rejects.toMatchObject({
        uncertain: true,
      });
      expect(fetch).toHaveBeenCalledTimes(1);
      expect(fetch.mock.calls[0][1]).toMatchObject({
        method: "POST",
        redirect: "error",
        cache: "no-store",
      });
    },
  );
  it("rejects authentication errors and does not follow login redirects", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response("{}", { status: 401 })),
    );
    await expect(query("/clusters")).rejects.toBeInstanceOf(ApiError);
  });
});
