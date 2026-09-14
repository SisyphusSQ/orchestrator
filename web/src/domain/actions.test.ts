import endpoints from "../api/endpoints.json";
import { expect, it } from "vitest";
import { actions, type Values } from "./actions";

it("every Web operation resolves to a registered Go route with a POST alias", () => {
  const paths = endpoints.filter((entry) => entry.method === "POST" && !entry.readOnly).map((entry) => entry.path.slice("/api/".length));
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
