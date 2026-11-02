import com.macro.mall.portal.config.LoadingPaymentPaths;

public class PaymentPathsTest {
    public static void main(String[] args) {
        for (String path : new String[]{"/alipay", "/alipay/webPay", "/alipay/notify",
                "/order/paySuccess", "/order/paySuccess/", "/order/paySuccess;session=x"}) {
            if (!LoadingPaymentPaths.isBlocked(path)) {
                throw new AssertionError("payment route is not blocked: " + path);
            }
        }
        for (String path : new String[]{"/actuator/health", "/sso/register", "/home/content",
                "/order/generateOrder", "/order/cancelUserOrder", "/alipay-other"}) {
            if (LoadingPaymentPaths.isBlocked(path)) {
                throw new AssertionError("nonpayment route is blocked: " + path);
            }
        }
        System.out.println("Payment path checks passed");
    }
}
