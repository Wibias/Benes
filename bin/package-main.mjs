export const packageName = "benes";
export const cliCommand = "benes";

export async function loadApi() {
  throw new Error(
    "The benes programmatic TypeScript API is retired. Use the `benes` CLI, which execs the Go runtime.",
  );
}
