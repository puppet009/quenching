import React from "react";
import ReactDOM from "react-dom/client";
import { RouterProvider } from "react-router-dom";
import { router } from "./app/router";
import "./styles/index.css";
import { startBrowserRuntime } from "@quenching/shared";

ReactDOM.createRoot(document.getElementById("root")!, startBrowserRuntime("admin")).render(
  <React.StrictMode>
    <RouterProvider router={router} />
  </React.StrictMode>
);
