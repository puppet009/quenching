import com.macro.mall.portal.config.SandboxPaymentSignature;
import java.nio.charset.StandardCharsets;
public class SandboxPaymentContractTest {
 public static void main(String[] args) {
  String key="xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx";
  byte[] body="{\"order_id\":\"8\"}".getBytes(StandardCharsets.UTF_8);
  String signature="1ef7e8a88d1beabe0f49b5de693a483f99d6f3d41ea8e7e4ec68ecb98123af9d";
  if(!SandboxPaymentSignature.verify(key,1000,body,signature,1000))throw new AssertionError("cross-language signature rejected");
  if(SandboxPaymentSignature.verify(key,1000,"tampered".getBytes(StandardCharsets.UTF_8),signature,1000))throw new AssertionError("tampered body accepted");
  if(SandboxPaymentSignature.verify(key,1000,body,signature,1121))throw new AssertionError("expired callback accepted");
  if(SandboxPaymentSignature.verify(key,1000,body,"bad hex",1000))throw new AssertionError("malformed signature accepted");
 }
}
