package com.macro.mall.portal.config;

public final class LoadingPaymentPaths {
    private LoadingPaymentPaths() {}

    public static boolean isBlocked(String path) {
        String normalized = path.replaceAll(";[^/]*", "");
        return normalized.equals("/alipay") || normalized.startsWith("/alipay/")
                || normalized.equals("/order/paySuccess")
                || normalized.startsWith("/order/paySuccess/");
    }
}
