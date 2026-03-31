{{- define "paymentButtonScript" -}}
(function() {
  var buttonId = {{.ButtonID | quote}};

  function dispatchPaymentEvent(eventName, detail) {
    var event = new CustomEvent(eventName, {
      detail: detail,
      bubbles: true
    });
    window.dispatchEvent(event);
    console.log('Payment event dispatched:', eventName, detail);
  }

  function initButton() {
    if (typeof window.atlos === 'undefined') {
      setTimeout(initButton, 100);
      return;
    }

    var button = document.getElementById(buttonId);
    if (!button) {
      console.error('Payment button not found:', buttonId);
      return;
    }

    button.addEventListener('click', function() {
      try {
        var paymentConfig = {
          merchantId: {{.MerchantID | quote}},
          orderId: {{.OrderID | quote}},
          orderAmount: {{.Amount}},
          orderCurrency: {{.Currency | quote}},
          userName: {{.UserName | quote}},
          userEmail: {{.UserEmail | quote}},
          captureEmail: false,
          postbackUrl: {{.PostbackURL | quote}},
          {{if .RecurringAmount}}
          subscription: [{
            amount: {{.RecurringAmount}},
            unit: {{.RecurringUnit | quote}},
            interval: {{.RecurringInterval}},
            startInterval: 1
          }],
          {{end}}
          onSuccess: function(response) {
            console.log('Payment successful:', response);
            dispatchPaymentEvent('paymentSuccess', null);
          },
          onCanceled: function(response) {
            console.log('Payment canceled:', response);
            dispatchPaymentEvent('paymentCanceled', null);
          },
          onCompleted: function(response) {
            console.log('Payment completed:', response);
            dispatchPaymentEvent('paymentCompleted', null);
          },
          onError: function(error) {
            console.error('Payment error:', error);
            dispatchPaymentEvent('paymentError', { error: error.message || error });
          },
          language: 'en',
          theme: 'light'
        };

        window.atlos.Pay(paymentConfig);
      } catch (error) {
        console.error('Failed to initialize ATLOS payment:', error);
        dispatchPaymentEvent('paymentError', { error: error.message || error });
      }
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initButton);
  } else {
    initButton();
  }
})();
{{- end -}}
