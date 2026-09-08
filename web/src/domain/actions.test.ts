import { readFileSync } from "node:fs";
import { expect, it } from "vitest";
import { actions, type Values } from "./actions";

it("every Web operation resolves to a registered Go route with a POST alias", () => {
  const api = readFileSync(
    "../internal/http/api.go",
    "utf8",
  );
  const aliases = readFileSync(
    "../internal/http/web_actions.go",
    "utf8",
  );
  const synonymsBlock = api.match(/var apiSynonyms[\s\S]*?\n\}/)?.[0] || "";
  const synonyms = new Map(
    [...synonymsBlock.matchAll(/"([^"]+)":\s*"([^"]+)"/g)].map((match) => [
      match[1],
      match[2],
    ]),
  );
  const paths = [...api.matchAll(/registerAPIRequest(?:NoProxy)?\(m, "([^"]+)"/g)].flatMap(
    (match) => {
      const parts = match[1].split("/");
      return synonyms.has(parts[0])
        ? [match[1], [synonyms.get(parts[0]), ...parts.slice(1)].join("/")]
        : [match[1]];
    },
  );
  const allowed = new Set(
    [...aliases.matchAll(/"([^"]+)":\s*true/g)].map((match) => match[1]),
  );
  for (const action of actions) {
    const values: Values = Object.fromEntries(
      action.fields.map((field) => [
        field.name,
        field.initial ?? (field.type === "number" ? 3306 : "example / + %"),
      ]),
    );
    const path = action
      .path(
        {
          instance: { Hostname: "mysql.test", Port: 3306 },
          cluster: "mysql.test:3306",
          agent: "agent.test",
          recovery: 7,
          seed: 8,
        },
        values,
      )
      .split("?")[0]
      .slice(1);
    expect(
      allowed.has(path.split("/")[0]),
      `${action.id} must have a POST alias`,
    ).toBe(true);
    expect(
      paths.some((pattern) =>
        new RegExp(
          "^" +
            pattern
              .split("/")
              .map((part) => (part.startsWith(":") ? "[^/]+" : part))
              .join("/") +
            "$",
        ).test(path),
      ),
      `${action.id}: /api/${path} is not registered`,
    ).toBe(true);
  }
});
