import js from "@eslint/js";
import { defineConfig } from "eslint/config";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

const frontendFiles = [
  "apps/web/**/*.{ts,tsx}",
  "apps/admin/**/*.{ts,tsx}",
  "packages/shared/**/*.{ts,tsx}",
];

export default defineConfig([
  {
    ignores: ["**/node_modules/**", "**/dist/**", "**/build/**", "**/generated/**", "**/coverage/**"],
  },
  {
    files: frontendFiles,
    extends: [js.configs.recommended, tseslint.configs.recommended],
    languageOptions: { globals: globals.browser },
    rules: {
      // Match TypeScript's unused-local checks without requiring unused catch bindings.
      "@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_", caughtErrors: "none" }],
    },
  },
  {
    files: frontendFiles,
    plugins: { "react-hooks": reactHooks },
    rules: {
      // Core Hooks checks apply without opting the applications into React Compiler.
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "warn",
    },
  },
  {
    files: ["apps/{web,admin}/vite.config.ts"],
    languageOptions: { globals: globals.node },
  },
  {
    files: ["eslint.config.mjs", "apps/{web,admin}/tests/**/*.mjs", "packages/shared/tests/**/*.mjs"],
    extends: [js.configs.recommended],
    languageOptions: { globals: globals.node },
  },
]);
