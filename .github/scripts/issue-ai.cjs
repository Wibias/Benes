"use strict";

const client = require("./issue-translator-ai-client.cjs");

module.exports = {
  ...client,
  completeJson: client.requestJsonCompletion,
};
