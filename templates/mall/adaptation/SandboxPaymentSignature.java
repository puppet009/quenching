package com.macro.mall.portal.config;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.HexFormat;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
public final class SandboxPaymentSignature {
 private SandboxPaymentSignature() {}
 public static boolean verify(String secret,long timestamp,byte[] body,String signature,long now) {
  if(secret==null || secret.length()<32 || timestamp<1 || timestamp<now-120 || timestamp>now+120)return false;
  try {
   Mac mac=Mac.getInstance("HmacSHA256");mac.init(new SecretKeySpec(secret.getBytes(StandardCharsets.UTF_8),"HmacSHA256"));
   mac.update((timestamp+"\n").getBytes(StandardCharsets.UTF_8));
   return MessageDigest.isEqual(mac.doFinal(body),HexFormat.of().parseHex(signature));
  }catch(Exception e){return false;}
 }
}
