import { useId, useState, type InputHTMLAttributes } from "react";

interface PasswordInputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, "type"> {
  passwordLabel?: string;
}

/** 密码默认隐藏，各输入框独立控制可见性。 */
export function PasswordInput({
  id,
  className = "",
  style,
  disabled,
  passwordLabel = "密码",
  ...props
}: PasswordInputProps) {
  const generatedId = useId();
  const inputId = id ?? generatedId;
  const [visible, setVisible] = useState(false);
  const toggleLabel = `${visible ? "隐藏" : "显示"}${passwordLabel}`;

  return (
    <div className="relative">
      <input
        {...props}
        id={inputId}
        type={visible ? "text" : "password"}
        className={`form-input ${className}`}
        style={{ ...style, paddingRight: "3rem" }}
        disabled={disabled}
      />
      <button
        type="button"
        aria-label={toggleLabel}
        aria-controls={inputId}
        aria-pressed={visible}
        title={toggleLabel}
        disabled={disabled}
        onClick={() => setVisible((current) => !current)}
        className="absolute inset-y-0 right-0 w-11 flex items-center justify-center rounded-lg text-ink-500 hover:text-ink-700 focus-visible:outline-2 focus-visible:outline-brand-500 focus-visible:-outline-offset-4 disabled:opacity-50"
      >
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
          {visible ? (
            <>
              <path d="m3 3 18 18M10.6 10.6a2 2 0 0 0 2.8 2.8M9.9 5.2A11 11 0 0 1 12 5c5.5 0 9 7 9 7a17.6 17.6 0 0 1-3 3.9M6.1 6.1C4.2 7.5 3 9.5 3 12c0 0 3.5 7 9 7a10.2 10.2 0 0 0 5-1.4" />
            </>
          ) : (
            <>
              <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z" />
              <circle cx="12" cy="12" r="3" />
            </>
          )}
        </svg>
      </button>
    </div>
  );
}
