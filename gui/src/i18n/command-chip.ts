import { createElement, Fragment } from "react";
import type { TKey } from "./en.ts";
import { useT } from "./hooks.ts";
import type { MessageVars } from "./message.ts";

const COMMAND_MARK = "{cmd}";

function chip(command: string) {
  return createElement("code", { className: "chip" }, command);
}

export function Trans({ k, cmd, vars }: { k: TKey; cmd: string; vars?: MessageVars }) {
  const text = useT()(k, vars);
  const at = text.indexOf(COMMAND_MARK);
  if (at === -1) return createElement(Fragment, null, text, chip(cmd));
  return createElement(Fragment, null, text.slice(0, at), chip(cmd), text.slice(at + COMMAND_MARK.length));
}
