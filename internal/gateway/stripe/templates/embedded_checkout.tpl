(function() {
	var stripe = globalThis.Stripe('{{.PublishableKey}}');

	async function initialize() {
		var checkoutInstance = await stripe.createEmbeddedCheckoutPage({
			clientSecret: '{{.ClientSecret}}',
			onComplete: function() {
				window.dispatchEvent(new CustomEvent('paymentCompleted', {
					bubbles: true
				}));
			}
		});

		(globalThis._PAYMENT_METHODS = globalThis._PAYMENT_METHODS || {})['stripe'] = checkoutInstance;
		checkoutInstance.mount('#stripe-checkout');
	}

	// Register cleanup function in global array
	var cleanup = function() {
		var checkoutInstance = globalThis._PAYMENT_METHODS && globalThis._PAYMENT_METHODS['stripe'];
		if (checkoutInstance) {
			checkoutInstance.destroy();
		}
		var container = document.getElementById('stripe-checkout');
		if (container) { container.innerHTML = ''; }
		var scripts = document.querySelectorAll('script[src*="js.stripe.com"]');
		scripts.forEach(function(s) { s.remove(); });
		delete globalThis.Stripe;
		delete globalThis._PAYMENT_METHODS['stripe'];
	};

	(globalThis.__PAYMENT_CLEANUP = globalThis.__PAYMENT_CLEANUP || []).push(cleanup);

	initialize();
})();
