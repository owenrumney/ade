// Everything here is the add-on except the tooling that builds it.
export default {
  ignoreFiles: ["package.json", "package-lock.json", "web-ext-config.mjs", "node_modules", "web-ext-artifacts"],
  build: { overwriteDest: true },
  sign: { channel: "unlisted" },
};
