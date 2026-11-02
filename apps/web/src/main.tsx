import React from "react";
import ReactDOM from "react-dom/client";
import { RouterProvider } from "react-router-dom";
import { router } from "./app/router";
import { AuthProvider } from "./auth/auth";
import "./styles/index.css";
import { startBrowserRuntime } from "@quenching/shared";

ReactDOM.createRoot(document.getElementById("root")!, startBrowserRuntime("web")).render(
  <React.StrictMode>
    <AuthProvider>
      <RouterProvider router={router} />
    </AuthProvider>
  </React.StrictMode>
);
