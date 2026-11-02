package com.macro.mall.portal.config;

import jakarta.servlet.FilterChain;
import jakarta.servlet.ServletException;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import java.io.IOException;
import org.springframework.context.annotation.Profile;
import org.springframework.core.Ordered;
import org.springframework.core.annotation.Order;
import org.springframework.stereotype.Component;
import org.springframework.web.filter.OncePerRequestFilter;

/** Disable both SDK payment and the upstream direct-pay shortcut in loading mode. */
@Component
@Profile("sandbox")
@Order(Ordered.HIGHEST_PRECEDENCE)
public class LoadingStagePaymentFilter extends OncePerRequestFilter {
    @Override
    protected void doFilterInternal(HttpServletRequest request, HttpServletResponse response,
                                    FilterChain chain) throws ServletException, IOException {
        if (LoadingPaymentPaths.isBlocked(request.getServletPath())) {
            response.setStatus(HttpServletResponse.SC_SERVICE_UNAVAILABLE);
            response.setContentType("application/json");
            response.setCharacterEncoding("UTF-8");
            response.getWriter().write("{\"code\":503,\"message\":\"Payment is unavailable during loading validation\"}");
            return;
        }
        chain.doFilter(request, response);
    }
}
