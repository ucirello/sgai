import { mock } from "bun:test";
import { GlobalRegistrator } from "@happy-dom/global-registrator";

mock.module("@/assets/sgai-logo.svg", () => ({
  default: "/assets/sgai-logo.svg",
}));

GlobalRegistrator.register();
