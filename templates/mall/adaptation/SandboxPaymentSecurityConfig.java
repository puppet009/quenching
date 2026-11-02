package com.macro.mall.portal.config;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.context.annotation.Profile;
import org.springframework.core.annotation.Order;
import org.springframework.security.config.annotation.web.builders.HttpSecurity;
import org.springframework.security.web.SecurityFilterChain;
@Configuration
@Profile("sandbox")
public class SandboxPaymentSecurityConfig {
 @Bean @Order(0)
 public SecurityFilterChain sandboxPaymentChain(HttpSecurity http) throws Exception {
  return http.securityMatcher("/sandbox/payment/**").csrf(csrf->csrf.disable()).authorizeHttpRequests(auth->auth.anyRequest().permitAll()).build();
 }
}
