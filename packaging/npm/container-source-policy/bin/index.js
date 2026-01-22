#!/usr/bin/env node

const { spawn } = require('child_process');
const { getExePath } = require('../get-exe');

const command_args = process.argv.slice(2);

const child = spawn(
    getExePath(),
    command_args,
    { stdio: "inherit" });

child.on('close', function (code) {
    if (code !== 0) {
        process.exit(code);
    }
});
