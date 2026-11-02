package com.macro.mall.portal.config;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.macro.mall.common.api.CommonResult;
import com.macro.mall.portal.service.OmsPortalOrderService;
import java.math.BigDecimal;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.util.LinkedHashMap;
import java.util.Map;
import javax.sql.DataSource;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Profile;
import org.springframework.http.HttpStatus;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.transaction.PlatformTransactionManager;
import org.springframework.transaction.support.TransactionTemplate;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.server.ResponseStatusException;

@RestController
@Profile("sandbox")
@RequestMapping("/sandbox/payment")
public class SandboxPaymentController {
    private final JdbcTemplate jdbc;
    private final TransactionTemplate transactions;
    private final OmsPortalOrderService orders;
    private final ObjectMapper mapper;
    @Value("${quenching.sandbox-payment-secret:}") private String secret;
    public SandboxPaymentController(DataSource source, PlatformTransactionManager manager, OmsPortalOrderService orders, ObjectMapper mapper) {
        this.jdbc = new JdbcTemplate(source); this.transactions = new TransactionTemplate(manager);
        this.orders = orders; this.mapper = mapper;
    }
    private void authorize(String timestamp,String signature,byte[] body) {
        try {
            if(!SandboxPaymentSignature.verify(secret,Long.parseLong(timestamp),body,signature,Instant.now().getEpochSecond())) throw new IllegalArgumentException();
        } catch(Exception e) { throw new ResponseStatusException(HttpStatus.FORBIDDEN,"sandbox payment authorization required"); }
    }
    private Map<String, Object> facts(Long id, boolean lock) {
        Map<String, Object> row = jdbc.queryForMap("SELECT id, member_id, pay_amount, status FROM oms_order WHERE id=? AND delete_status=0" + (lock ? " FOR UPDATE" : ""), id);
        Map<String, Object> out = new LinkedHashMap<>();
        out.put("order_id", row.get("id").toString()); out.put("account_ref", row.get("member_id").toString());
        out.put("amount_minor", new BigDecimal(row.get("pay_amount").toString()).movePointRight(2).longValueExact());
        out.put("currency", "CNY"); out.put("status", ((Number) row.get("status")).intValue());
        var receipts = jdbc.queryForList("SELECT receipt_id FROM quenching_sandbox_payment_receipts WHERE order_id=?", id);
        out.put("receipt_id", receipts.isEmpty() ? "" : receipts.get(0).get("receipt_id").toString()); return out;
    }
    @GetMapping("/orders/{id}")
    public CommonResult<Map<String, Object>> inspect(@PathVariable Long id, @RequestHeader("X-Sandbox-Payment-Timestamp") String timestamp, @RequestHeader("X-Sandbox-Payment-Signature") String signature) {
        authorize(timestamp, signature, ("GET\n/sandbox/payment/orders/" + id).getBytes(StandardCharsets.UTF_8));
        return CommonResult.success(facts(id, false));
    }
    @PostMapping("/callback")
    public CommonResult<Map<String, Object>> callback(@RequestBody byte[] body, @RequestHeader("X-Sandbox-Payment-Timestamp") String timestamp, @RequestHeader("X-Sandbox-Payment-Signature") String signature) throws Exception {
        authorize(timestamp, signature, body);
        JsonNode claim = mapper.readTree(body);
        String commandId = claim.path("command_id").asText();
        if (!commandId.matches("[A-Za-z0-9_.:-]{1,128}") || !claim.path("currency").asText().equals("CNY")) throw new ResponseStatusException(HttpStatus.BAD_REQUEST);
        Long id = Long.valueOf(claim.path("order_id").asText());
        Map<String, Object> result = transactions.execute(status -> {
            Map<String, Object> before = facts(id, true);
            if (!before.get("account_ref").equals(claim.path("account_ref").asText()) || ((Number) before.get("amount_minor")).longValue() != claim.path("amount_minor").asLong()) throw new ResponseStatusException(HttpStatus.CONFLICT, "order facts mismatch");
            var previous = jdbc.queryForList("SELECT command_id FROM quenching_sandbox_payment_receipts WHERE order_id=?", id);
            if (!previous.isEmpty()) {
                if (!previous.get(0).get("command_id").equals(commandId)) throw new ResponseStatusException(HttpStatus.CONFLICT, "order already paid");
                return before;
            }
            if (((Number) before.get("status")).intValue() != 0) throw new ResponseStatusException(HttpStatus.CONFLICT, "order is not payable");
            jdbc.update("INSERT INTO quenching_sandbox_payment_receipts(command_id,order_id,receipt_id) VALUES(?,?,?)", commandId, id, "sandbox-" + commandId);
            orders.paySuccess(id, 2); // Preserve upstream order and stock business logic within this transaction.
            Map<String, Object> after = facts(id, false);
            if (((Number) after.get("status")).intValue() != 1) throw new ResponseStatusException(HttpStatus.CONFLICT, "payment not applied");
            return after;
        });
        return CommonResult.success(result);
    }
}
